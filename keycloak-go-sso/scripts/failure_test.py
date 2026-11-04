"""Disruptive local lab test: stops Keycloak and restarts API B; restores Keycloak in finally."""

import base64
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request

from browser_test import Browser


def call(port, token):
    """API /me を呼び、ステータスコードだけを返す。"""
    request = urllib.request.Request(
        f'http://127.0.0.1:{port}/api/me',
        headers={'Authorization': 'Bearer ' + token},
    )
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code


def ready(address):
    """指定URLが200を返すまで最大120秒待つ。"""
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(address, timeout=3) as response:
                if response.status == 200:
                    return
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            pass
        time.sleep(1)
    raise RuntimeError('Service readiness timeout')


def main():
    """Keycloakを止めて、warm/cold cache、新規ログイン、更新の結果を確認する。"""
    base_a, base_b = 'http://127.0.0.1:9081', 'http://127.0.0.1:9082'

    # 停止前にログインし、A/Bの両APIで鍵をキャッシュさせる。
    browser = Browser()
    browser.login(base_a, 'alice')
    browser.login(base_b)
    token_a = browser.action(base_a, 'export')
    token_b = browser.action(base_b, 'export')
    assert call(9001, token_a) == 200 and call(9002, token_b) == 200

    try:
        # warm cache：鍵はAPIのメモリにあるので、停止後も200のまま。
        subprocess.run(['docker', 'compose', 'stop', 'keycloak'], check=True)
        assert call(9001, token_a) == 200 and call(9002, token_b) == 200
        print('PASS IdP down: warm JWKS A/B -> 200', flush=True)

        # 新規ログイン：Keycloakへ到達できず失敗する。
        try:
            Browser().get(base_a + '/login')
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            print('PASS IdP down: new login unavailable', flush=True)
        else:
            raise AssertionError('New login unexpectedly available')

        # 更新：token endpointへ到達できず失敗する。
        assert 'Refresh unavailable' in browser.action(base_a, 'refresh')
        print('PASS IdP down: refresh unavailable', flush=True)

        # cold cache：再起動でBの鍵キャッシュが消え、Bだけ401になる。Aは比較対象。
        subprocess.run(['docker', 'compose', 'restart', 'api-b'], check=True)
        ready('http://127.0.0.1:9002/healthz')
        assert call(9002, token_b) == 401 and call(9001, token_a) == 200
        print('PASS IdP down: cold B -> 401, warm A -> 200', flush=True)

        # キャッシュがあっても、実際のexpを過ぎたトークンは拒否される。
        if '--expiry' in sys.argv:
            payload = token_a.split('.')[1]
            expires = json.loads(base64.urlsafe_b64decode(payload + '=' * (-len(payload) % 4)))['exp']

            while time.time() <= expires:
                print('Waiting for genuine token expiry...', flush=True)
                time.sleep(min(30, max(1, expires + 1 - time.time())))

            assert call(9001, token_a) == 401
            print('PASS IdP down: expired token with warm cache -> 401', flush=True)

    finally:
        # 途中で失敗しても、必ずKeycloakを復旧する。
        subprocess.run(['docker', 'compose', 'start', 'keycloak'], check=True)
        ready('http://127.0.0.1:9080/realms/sso-lab/.well-known/openid-configuration')
        print('Keycloak restored', flush=True)


if __name__ == '__main__':
    main()
