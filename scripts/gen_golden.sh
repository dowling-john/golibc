#!/bin/sh
# Regenerates the golden files in pkg/golibc/testdata from real glibc:
#   printf.golden  from printf.cases  (glibc printf)
#   scan.golden    from scan.cases    (glibc sscanf)
#   strftime.golden from strftime.cases (glibc gmtime_r + strftime, TZ unset = UTC)
#   gmtime.golden  from gmtime.cases  (glibc gmtime_r)
#   localtime.golden from localtime.cases (glibc localtime_r + mktime under various TZ)
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

# ---- strftime: "timestamp<TAB>format" ----
def cstr(x):
    return x.replace('\\', '\\\\').replace('"', '\\"')
out = ['#include <stdio.h>', '#include <string.h>', '#include <time.h>', 'int main(void){', 'char buf[512]; struct tm tm; time_t t; size_t n;']
for line in cases(f'{d}/strftime.cases'):
    ts, _, fmt = line.partition('\t')
    out.append(f'memset(buf,0,sizeof buf); t={ts}LL; gmtime_r(&t,&tm); n=strftime(buf,sizeof buf,"{cstr(fmt)}",&tm); printf("%zu [%s]\\n",n,buf);')
out.append('return 0;}')
open(f'{w}/strftime.c', 'w').write('\n'.join(out))

# ---- gmtime ----
out = ['#include <stdio.h>', '#include <time.h>', 'int main(void){', 'struct tm tm; struct tm *r; time_t t;']
for line in cases(f'{d}/gmtime.cases'):
    out.append(f't={line}LL; r=gmtime_r(&t,&tm); if(!r) puts("null"); else printf("%d %d %d %d %d %d %d %d %d\\n",tm.tm_sec,tm.tm_min,tm.tm_hour,tm.tm_mday,tm.tm_mon,tm.tm_year,tm.tm_wday,tm.tm_yday,tm.tm_isdst);')
out.append('return 0;}')
open(f'{w}/gmtime.c', 'w').write('\n'.join(out))

# ---- localtime / mktime under a TZ per case ----
show = 'printf("%d %d %d %d %d %d %d %d %d %ld %s\\n",tm.tm_sec,tm.tm_min,tm.tm_hour,tm.tm_mday,tm.tm_mon,tm.tm_year,tm.tm_wday,tm.tm_yday,tm.tm_isdst,(long)tm.tm_gmtoff,tm.tm_zone);'
out = ['#include <stdio.h>', '#include <stdlib.h>', '#include <time.h>', 'int main(void){', 'struct tm tm; time_t t;']
for line in cases(f'{d}/localtime.cases'):
    f = line.split('\t')
    tz, kind = f[0], f[1]
    out.append(f'setenv("TZ","{tz}",1); tzset();')
    if kind == 'l':
        out.append(f't={f[2]}LL; localtime_r(&t,&tm); {show}')
    else:
        y, mo, da, h, mi, se = f[2:8]
        out.append(f'tm=(struct tm){{.tm_year={y}-1900,.tm_mon={mo}-1,.tm_mday={da},.tm_hour={h},.tm_min={mi},.tm_sec={se},.tm_isdst=-1}}; t=mktime(&tm); printf("t=%ld ",(long)t); {show}')
out.append('return 0;}')
open(f'{w}/localtime.c', 'w').write('\n'.join(out))
PY

docker run --rm -v "$work":/w golang:1.23 sh -c '
  gcc -w -o /printf /w/printf.c -lm && /printf > /w/printf.golden &&
  gcc -w -o /scan /w/scan.c && /scan > /w/scan.golden &&
  gcc -w -o /strftime /w/strftime.c && /strftime > /w/strftime.golden &&
  gcc -w -o /gmtime /w/gmtime.c && /gmtime > /w/gmtime.golden &&
  gcc -w -o /localtime /w/localtime.c && /localtime > /w/localtime.golden'
cp "$work/printf.golden" "$work/scan.golden" "$work/strftime.golden" "$work/gmtime.golden" "$work/localtime.golden" "$dir/"
wc -l "$dir"/*.golden
