#!/bin/sh
# Regenerates the golden files in pkg/golibc/testdata from real glibc:
#   printf.golden  from printf.cases  (glibc printf)
#   scan.golden    from scan.cases    (glibc sscanf)
# Needs docker. Run from the repo root (or use `make golden`).
set -e
dir=pkg/golibc/testdata
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

DIR="$dir" WORK="$work" python3 - <<'PY'
import os
d, w = os.environ['DIR'], os.environ['WORK']

def cases(path):
    for line in open(path):
        line = line.rstrip('\n')
        if line and not line.startswith('#'):
            yield line

# ---- printf: "format|kind:value,kind:value" ----
out = ['#include <stdio.h>', '#include <math.h>', 'int main(void){']
for line in cases(f'{d}/printf.cases'):
    fmt, _, args = line.partition('|')
    cargs = []
    for a in filter(None, args.split(',')):
        k, _, v = a.partition(':')
        if k in ('i', 'c'): cargs.append(f'(int)({v})')
        elif k == 'l': cargs.append(f'(long)({v}L)')
        elif k == 'u': cargs.append(f'(unsigned)({v}U)')
        elif k == 'ul': cargs.append(f'(unsigned long)({v}UL)')
        elif k == 's': cargs.append(f'"{v}"')
        elif k == 'd':
            cargs.append({'inf': 'INFINITY', '-inf': '-INFINITY', 'nan': 'NAN'}.get(v, f'(double)({v})'))
    out.append('printf("%s\\n"%s);' % (fmt.replace('"', '\\"'), ''.join(', ' + c for c in cargs)))
out.append('return 0;}')
open(f'{w}/printf.c', 'w').write('\n'.join(out))

# ---- scanf: "input<TAB>format<TAB>kinds" ----
# kind -> (declaration, printf format, is array)
K = {
    'i':  ('int v=-77777;',           '%d',    False),
    'l':  ('long v=-77777;',          '%ld',   False),
    'h':  ('short v=-7777;',          '%d',    False),
    'hh': ('signed char v=-77;',      '%d',    False),
    'u':  ('unsigned v=77777u;',      '%u',    False),
    'ul': ('unsigned long v=77777ul;', '%lu',  False),
    'd':  ('double v=-7.5;',          '%.17g', False),
    'f':  ('float v=-7.5f;',          '%.9g',  False),
    's':  ('char v[64]={0};',         '[%s]',  True),
    'c':  ('char v[9]="????????";',   '[%s]',  True),
}
out = ['#include <stdio.h>', 'int main(void){']
for line in cases(f'{d}/scan.cases'):
    inp, fmt, kinds = line.split('\t')
    kinds = [k for k in kinds.split(',') if k]
    decls, addrs, fmts, vals = [], [], [], []
    for n, k in enumerate(kinds):
        decl, pf, arr = K[k]
        decls.append(decl.replace(' v', f' v{n}', 1).replace('v[', f'v{n}[', 1) if arr else decl.replace('v=', f'v{n}=', 1))
        addrs.append(f'v{n}' if arr else f'&v{n}')
        fmts.append(pf)
        vals.append(f'v{n}')
    # "%*d" suppresses an argument, so kinds only list assigned items.
    call = f'sscanf("{inp}", "{fmt}"' + ''.join(', ' + a for a in addrs) + ')'
    out.append('{ ' + ' '.join(decls) + f' int r = {call}; printf("ret=%d' + ''.join(' ' + f for f in fmts) + '\\n", r' + ''.join(', ' + v for v in vals) + '); }')
out.append('return 0;}')
open(f'{w}/scan.c', 'w').write('\n'.join(out))
PY

docker run --rm -v "$work":/w golang:1.23 sh -c '
  gcc -w -o /printf /w/printf.c -lm && /printf > /w/printf.golden &&
  gcc -w -o /scan /w/scan.c && /scan > /w/scan.golden'
cp "$work/printf.golden" "$work/scan.golden" "$dir/"
wc -l "$dir"/*.golden
