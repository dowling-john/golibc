#!/bin/sh
# Regenerates pkg/golibc/testdata/printf.golden from real glibc printf (needs docker).
# Usage: scripts/gen_golden.sh   (run from the repo root)
set -e
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
python3 - <<'PY' > "$tmp"
import sys
print('#include <stdio.h>\n#include <math.h>\nint main(void){')
for line in open('pkg/golibc/testdata/printf.cases'):
    line = line.rstrip('\n')
    if not line or line.startswith('#'):
        continue
    fmt, _, args = line.partition('|')
    cargs = []
    for a in filter(None, args.split(',')):
        k, _, v = a.partition(':')
        if k == 'i' or k == 'c': cargs.append('(int)(%s)' % v)
        elif k == 'l': cargs.append('(long)(%sL)' % v)
        elif k == 'u': cargs.append('(unsigned)(%sU)' % v)
        elif k == 'ul': cargs.append('(unsigned long)(%sUL)' % v)
        elif k == 's': cargs.append('"%s"' % v)
        elif k == 'd':
            cargs.append({'inf': 'INFINITY', '-inf': '-INFINITY', 'nan': 'NAN'}.get(v, '(double)(%s)' % v))
    print('printf("%s\\n"%s);' % (fmt.replace('"', '\\"'), ''.join(', ' + c for c in cargs)))
print('return 0;}')
PY
docker run --rm -v "$tmp":/c.c golang:1.23 sh -c 'gcc -w -o /c /c.c -lm && /c' > pkg/golibc/testdata/printf.golden
wc -l pkg/golibc/testdata/printf.golden
