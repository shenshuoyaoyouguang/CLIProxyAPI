import json
with open('lint_full.json', 'r', encoding='utf-8') as f:
    data = json.load(f)
for i, issue in enumerate(data.get('Issues', [])):
    pos = issue.get('Pos', {})
    print(f"{i+1}. [{issue.get('FromLinter')}] {pos.get('Filename')}:{pos.get('Line')} -> {issue.get('Text')}")
