# 起動順序を制御して全サービスを起動する。

import subprocess
import time
import urllib.error
import urllib.request

# WebはDiscoveryが必須なので、先にDBとKeycloakだけ起動する。
subprocess.run(['docker', 'compose', 'up', '-d', 'postgres', 'keycloak'], check=True)

# コンテナの起動ではなく、Discoveryが200を返すことを最大120秒待つ。
deadline = time.monotonic() + 120
while True:
    try:
        with urllib.request.urlopen(
            'http://127.0.0.1:9080/realms/sso-lab/.well-known/openid-configuration',
            timeout=3,
        ) as response:
            if response.status == 200:
                break
    except (urllib.error.URLError, TimeoutError, ConnectionError):
        pass

    if time.monotonic() > deadline:
        raise SystemExit('Keycloak not ready. Check docker compose logs keycloak postgres.')

    time.sleep(1)

# Keycloakの準備ができてからWeb/APIをビルドして起動する。
subprocess.run(
    ['docker', 'compose', 'up', '-d', '--build', 'api-a', 'api-b', 'web-a', 'web-b'],
    check=True,
)

print('Open http://127.0.0.1:9081/ and http://127.0.0.1:9082/')
