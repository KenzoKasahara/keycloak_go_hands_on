"""Protocol integration test with a cookie jar, not a substitute for real browser verification."""

import http.cookiejar
import pathlib
import urllib.error
import urllib.parse
import urllib.request
from html.parser import HTMLParser

root = pathlib.Path(__file__).resolve().parents[1]
values = dict(
    line.split('=', 1)
    for line in (root / '.env').read_text().splitlines()
    if '=' in line
)


class Forms(HTMLParser):
    """KeycloakのログインフォームのAction先と、アプリのCSRFトークンをHTMLから取り出す。"""

    def __init__(self):
        super().__init__()
        self.action = None
        self.csrf = None

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == 'form' and attrs.get('id') == 'kc-form-login':
            self.action = attrs['action']
        if tag == 'input' and attrs.get('name') == 'csrf':
            self.csrf = attrs['value']


class LoopbackPolicy(http.cookiejar.DefaultCookiePolicy):
    """ブラウザがループバックを信頼する挙動を模擬するCookieポリシー。"""

    def return_ok_secure(self, cookie, request):
        if urllib.parse.urlparse(request.full_url).hostname == '127.0.0.1':
            return True
        return super().return_ok_secure(cookie, request)


class Browser:
    """Cookie jarを持つ簡易クライアント。インスタンスごとに別のブラウザプロファイルに相当する。"""

    def __init__(self):
        cookies = http.cookiejar.CookieJar(LoopbackPolicy())
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))

    def get(self, address):
        with self.opener.open(address, timeout=15) as response:
            return response.read().decode()

    def login(self, base, username=None):
        """ログインを行う。usernameがNoneならSSOでフォームが出ないことを確認する。"""
        body = self.get(base + '/login')
        parser = Forms()
        parser.feed(body)

        if username is None:
            assert parser.action is None and 'sub:' in body, 'SSO did not skip login form'
        else:
            assert parser.action, 'Expected Keycloak login form'
            data = urllib.parse.urlencode({
                'username': username,
                'password': values[username.upper() + '_PASSWORD'],
                'credentialId': '',
            }).encode()
            request = urllib.request.Request(parser.action, data=data)
            with self.opener.open(request, timeout=15) as response:
                body = response.read().decode()
            assert 'sub:' in body, 'Login failed'

        return body

    def action(self, base, operation, **extra):
        """画面のボタン操作に相当するPOSTを送る。"""
        body = self.get(base + '/')
        parser = Forms()
        parser.feed(body)
        assert parser.csrf, 'No application session'

        # Originを固定値で送るので、実ブラウザのReferrer-Policyの影響はここでは再現されない。
        data = urllib.parse.urlencode(dict(csrf=parser.csrf, operation=operation, **extra)).encode()
        request = urllib.request.Request(base + '/action', data=data, headers={'Origin': base})
        with self.opener.open(request, timeout=15) as response:
            return response.read().decode()


def main():
    """SSO、API呼び出し、更新、ログアウトを順に確認し、seed/verify用のトークンを保存する。"""
    base_a, base_b = 'http://127.0.0.1:9081', 'http://127.0.0.1:9082'

    # SSO：Bではパスワードを送らずにログインできる。
    alice = Browser()
    alice.login(base_a, 'alice')
    alice.login(base_b)
    print('PASS protocol SSO A -> B without password submission to B')

    # API呼び出し、audience拒否、更新。
    for base in [base_a, base_b]:
        assert 'HTTP 200' in alice.action(base, 'me')
        assert 'HTTP 401' in alice.action(base, 'cross')
        assert 'Refresh HTTP 200' in alice.action(base, 'refresh')
    assert 'HTTP 200' in alice.action(base_a, 'admin')

    alice_a = alice.action(base_a, 'export')
    alice_b = alice.action(base_b, 'export')

    # bobは別のCookie jarを使う（別ブラウザプロファイルに相当）。
    bob = Browser()
    bob.login(base_a, 'bob')
    bob.login(base_b)
    assert 'HTTP 403' in bob.action(base_a, 'admin')
    bob_b = bob.action(base_b, 'export')

    # seed/verify用にトークンを保存する。
    tokens = root / 'generated'
    for name, token in [('alice-a.token', alice_a), ('alice-b.token', alice_b), ('bob-b.token', bob_b)]:
        target = tokens / name
        target.write_text(token)
        target.chmod(0o600)

    # ローカルログアウト後もIdPセッションが残るので、パスワードなしで再ログインできる。
    alice.action(base_a, 'local-logout')
    alice.login(base_a)
    print('PASS local logout retains IdP SSO')

    # SSOログアウト後も他アプリのローカルセッションは残るが、更新は拒否される。
    alice.action(base_a, 'sso-logout')
    assert 'sub:' in alice.get(base_b + '/'), 'Other local app session unexpectedly removed'
    assert 'Refresh HTTP 400' in alice.action(base_b, 'refresh')

    # IdPセッションがないので、次のログインではパスワードが必要になる。
    alice.action(base_b, 'local-logout')
    login_page = alice.get(base_b + '/login')
    parser = Forms()
    parser.feed(login_page)
    assert parser.action, 'Expected password form after IdP logout'
    print('PASS SSO logout removes IdP session, retains other local session, rejects refresh')

    print('Tokens for seed/verify are in generated/; valid for only 180 seconds.')


if __name__ == '__main__':
    main()
