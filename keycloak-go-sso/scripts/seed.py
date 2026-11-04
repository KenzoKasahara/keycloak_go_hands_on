# API Bで検証済みのsubを所有者として、documentsへ文書を投入する。既存の文書は消える。

import json
import pathlib
import subprocess
import sys
import urllib.request

if len(sys.argv) != 3:
    raise SystemExit('usage: python3 scripts/seed.py alice-web-b.token bob-web-b.token')

# JWTを自分でdecodeせず、API Bに検証させた結果のsubを使う。
subjects = []
for filename in sys.argv[1:]:
    token = pathlib.Path(filename).read_text().strip()
    request = urllib.request.Request(
        'http://127.0.0.1:9002/api/me',
        headers={'Authorization': 'Bearer ' + token},
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        subjects.append(json.load(response)['sub'])

if subjects[0] == subjects[1]:
    raise SystemExit('Use different users for Alice and Bob')

# 値はpsql変数のリテラル引用で渡し、SQLへ文字列連結しない。
script = """TRUNCATE documents RESTART IDENTITY;
INSERT INTO documents(owner_sub,title,body) VALUES
(:'alice','Alice document','Alice private body'),
(:'bob','Bob document','Bob private body');
"""

subprocess.run(
    [
        'docker', 'compose', 'exec', '-T', 'postgres',
        'psql', '-U', 'app', '-d', 'app',
        '-v', 'ON_ERROR_STOP=1',
        '-v', 'alice=' + subjects[0],
        '-v', 'bob=' + subjects[1],
    ],
    input=script,
    text=True,
    check=True,
)

print('Seeded document 1=Alice, document 2=Bob; previous documents replaced.')
