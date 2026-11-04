# 実サービスへHTTPリクエストを送り、期待するステータスコードを確認する結合試験。

import json
import pathlib
import sys
import urllib.error
import urllib.request

if len(sys.argv) != 4:
    raise SystemExit('usage: python3 scripts/verify.py alice-web-a.token alice-web-b.token bob-web-b.token')

alice_a, alice_b, bob_b = [pathlib.Path(name).read_text().strip() for name in sys.argv[1:]]


def call(port, path, token=None):
    """APIを呼び、ステータスとボディを返す。4xxも例外にせず結果として扱う。"""
    request = urllib.request.Request(f'http://127.0.0.1:{port}{path}')
    if token:
        request.add_header('Authorization', 'Bearer ' + token)

    try:
        response = urllib.request.urlopen(request, timeout=10)
    except urllib.error.HTTPError as error:
        response = error

    with response:
        return response.status, response.read().decode()


# （ポート, パス, トークン, 期待するステータス）の一覧。
cases = [
    # 正常系
    (9001, '/healthz', None, 200),
    (9002, '/healthz', None, 200),
    (9001, '/api/me', alice_a, 200),
    (9002, '/api/me', alice_b, 200),
    (9002, '/api/me', bob_b, 200),
    (9001, '/api/admin', alice_a, 200),
    (9002, '/api/documents/1', alice_b, 200),
    (9002, '/api/documents/2', bob_b, 200),
    (9002, '/api/documents/2', alice_b, 200),  # adminは他者の個別文書を取得できる

    # 拒否系
    (9001, '/api/me', None, 401),  # トークンなし
    (9002, '/api/me', alice_a, 401),  # audience違い
    (9001, '/api/me', alice_b, 401),  # audience違い
    (9002, '/api/admin', bob_b, 403),  # ロール不足
    (9002, '/api/documents/1', bob_b, 404),  # 他者の文書
    (9002, '/api/documents/999999', bob_b, 404),  # 存在しない文書
    (9002, '/api/documents/1%27OR%271%27%3D%271', bob_b, 400),  # 不正なID
]

for port, path, token, expected in cases:
    status, body = call(port, path, token)
    if status != expected:
        raise SystemExit(f'FAIL {port}{path}: expected {expected}, got {status}; {body}')
    print(f'PASS {port}{path}: {status}')

# 一覧はadminのaliceを含めて本人の文書だけになることを確認する。
for token, expected in [(alice_b, [1]), (bob_b, [2])]:
    status, body = call(9002, '/api/documents', token)
    if status != 200 or [item['id'] for item in json.loads(body)] != expected:
        raise SystemExit('FAIL owner-filtered listing: ' + body)

print('PASS owner-filtered listing (including admin)')
