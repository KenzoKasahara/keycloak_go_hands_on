# .env とRealm定義（generated/sso-lab-realm.json）を生成する。最初に一度だけ実行する。

import json
import os
import pathlib
import secrets
import sys

root = pathlib.Path(__file__).resolve().parents[1]
env = root / '.env'


# ---------------------------------------------------------------------------
# .env の生成
# ---------------------------------------------------------------------------

# DB Volumeと資格情報が食い違わないよう、既存の .env は上書きしない。
if env.exists() and '--realm-only' not in sys.argv:
    raise SystemExit('.env already exists; keep it together with generated/ and DB volumes.')

names = (root / '.env.example').read_text().splitlines()

# .env.example の変数ごとにランダム値を作り、所有者だけが読める権限で保存する。
if not env.exists():
    values = {
        line.split('=')[0]: secrets.token_urlsafe(32)
        for line in names
        if line and not line.startswith('#')
    }
    descriptor = os.open(env, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as output:
        output.write(''.join(f'{key}={value}\n' for key, value in values.items()))

generated = root / 'generated'
generated.mkdir(exist_ok=True)
generated.chmod(0o700)


# ---------------------------------------------------------------------------
# Realm全体の設定
# ---------------------------------------------------------------------------

realm = {
    'realm': 'sso-lab',
    'enabled': True,
    'sslRequired': 'none',

    # Access Tokenは期限切れを観察しやすいよう180秒にする。
    'accessTokenLifespan': 180,
    'ssoSessionIdleTimeout': 1800,

    'roles': {
        'realm': [
            {'name': 'admin'},
            {'name': 'user'},
        ],
    },
    'users': [],
    'clients': [],
}


# ---------------------------------------------------------------------------
# 共通ユーザー（aliceはadmin、bobは一般ユーザー）
# ---------------------------------------------------------------------------

for name, roles in [('alice', ['admin', 'user']), ('bob', ['user'])]:
    realm['users'].append({
        'username': name,
        'enabled': True,
        'emailVerified': True,
        'firstName': name,
        'lastName': 'Lab',
        'email': name + '@example.test',

        # 実パスワードは書かず、Keycloakが環境変数から展開するプレースホルダーだけを出力する。
        'credentials': [
            {
                'type': 'password',
                'value': '${' + name.upper() + '_PASSWORD}',
                'temporary': False,
            },
        ],
        'realmRoles': roles,
    })


# ---------------------------------------------------------------------------
# クライアント（アプリごとにWebとAPIを1組ずつ定義する）
# ---------------------------------------------------------------------------

for suffix, port in [('a', 9081), ('b', 9082)]:
    base = f'http://127.0.0.1:{port}'

    # Webクライアント：ブラウザのログインを担当する。
    realm['clients'].append({
        'clientId': 'web-' + suffix,
        'protocol': 'openid-connect',
        'enabled': True,

        # client secretを持たないPublic Client。認可コードフローだけを許可する。
        'publicClient': True,
        'standardFlowEnabled': True,
        'implicitFlowEnabled': False,
        'directAccessGrantsEnabled': False,
        'serviceAccountsEnabled': False,

        # 不要なロールをトークンに入れない（許可するロールはscopeMappingsで指定する）。
        'fullScopeAllowed': False,

        'redirectUris': [base + '/callback'],
        'webOrigins': [],
        'defaultClientScopes': ['basic', 'profile', 'email'],

        # PKCE S256を必須にし、ログアウト後の戻り先を限定する。
        'attributes': {
            'pkce.code.challenge.method': 'S256',
            'post.logout.redirect.uris': base + '/',
        },

        'protocolMappers': [
            # Access TokenのaudにこのアプリのAPIだけを入れる。
            {
                'name': 'only-this-api',
                'protocol': 'openid-connect',
                'protocolMapper': 'oidc-audience-mapper',
                'config': {
                    'included.custom.audience': 'api-' + suffix,
                    'access.token.claim': 'true',
                    'id.token.claim': 'false',
                },
            },
            # Access Tokenのrealm_access.rolesにロールを入れる。
            {
                'name': 'realm-roles',
                'protocol': 'openid-connect',
                'protocolMapper': 'oidc-usermodel-realm-role-mapper',
                'config': {
                    'claim.name': 'realm_access.roles',
                    'jsonType.label': 'String',
                    'multivalued': 'true',
                    'access.token.claim': 'true',
                    'id.token.claim': 'false',
                },
            },
        ],
    })

    # APIクライアント：audienceの宛先になるだけで、自身ではログインしない。
    realm['clients'].append({
        'clientId': 'api-' + suffix,
        'protocol': 'openid-connect',
        'enabled': True,
        'bearerOnly': True,
        'standardFlowEnabled': False,
        'directAccessGrantsEnabled': False,
    })

# fullScopeAllowed=Falseなので、Webクライアントに許可するロールを明示する。
realm['scopeMappings'] = [
    {'client': 'web-' + suffix, 'roles': ['admin', 'user']}
    for suffix in ['a', 'b']
]


# ---------------------------------------------------------------------------
# Realm定義の書き出し
# ---------------------------------------------------------------------------

target = generated / 'sso-lab-realm.json'
target.write_text(json.dumps(realm, indent=2) + '\n')
target.chmod(0o644)

print('Created .env and generated/sso-lab-realm.json. Read login passwords locally from .env.')
