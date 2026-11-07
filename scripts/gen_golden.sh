#!/bin/sh
# Regenerates the golden files in pkg/golibc/testdata from real glibc:
#   printf.golden  from printf.cases  (glibc printf)
#   scan.golden    from scan.cases    (glibc sscanf)
#   strftime.golden from strftime.cases (glibc gmtime_r + strftime, TZ unset = UTC)
#   gmtime.golden  from gmtime.cases  (glibc gmtime_r)
#   localtime.golden from localtime.cases (glibc localtime_r + mktime under various TZ)
#   random.golden  from random.cases  (glibc rand, random and the *rand48 family per seed)
#   strptime.golden from strptime.cases (glibc strptime into a zeroed struct tm)
#   tzset.golden   from tzset.cases   (glibc tzname, timezone and daylight after tzset)
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
# ---- random: one seed per line, "-" for the unseeded default state ----
out = ['#include <stdio.h>', '#include <stdlib.h>', 'int main(void){', 'unsigned short xs[3];']
for line in cases(f'{d}/random.cases'):
    seed = line.strip()
    out.append('{ int i;')
    if seed != '-':
        out.append(f'srand({seed}U); srand48((long){seed}U);')
    out.append(f'printf("{seed} rand:"); for(i=0;i<10;i++) printf(" %d", rand());')
    if seed != '-':
        out.append(f'srandom({seed}U);')
    out.append('printf(" random:"); for(i=0;i<5;i++) printf(" %ld", random());')
    out.append('printf(" lrand48:"); for(i=0;i<5;i++) printf(" %ld", lrand48());')
    out.append('printf(" mrand48:"); for(i=0;i<5;i++) printf(" %ld", mrand48());')
    out.append('printf(" drand48:"); for(i=0;i<3;i++) printf(" %.17g", drand48());')
    out.append('xs[0]=1; xs[1]=2; xs[2]=3; printf(" nrand48:"); for(i=0;i<3;i++) printf(" %ld", nrand48(xs)); printf(" xs=%u,%u,%u", xs[0], xs[1], xs[2]);')
    out.append('xs[0]=1; xs[1]=2; xs[2]=3; printf(" jrand48:"); for(i=0;i<3;i++) printf(" %ld", jrand48(xs));')
    out.append('xs[0]=1; xs[1]=2; xs[2]=3; printf(" erand48:"); for(i=0;i<3;i++) printf(" %.17g", erand48(xs));')
    out.append('printf("\\n"); }')
out.append('return 0;}')
open(f'{w}/random.c', 'w').write('\n'.join(out))
# ---- strptime: "input<TAB>format", with C escapes such as \n left for the compiler ----
def cesc(x):
    return x.replace('"', '\\"')
out = ['#define _GNU_SOURCE', '#include <stdio.h>', '#include <stdlib.h>', '#include <string.h>', '#include <time.h>',
       'int main(void){', 'struct tm tm; const char *in; const char *r;', 'setenv("TZ","UTC",1); tzset();']
for line in open(f'{d}/strptime.cases'):
    line = line.rstrip('\n')
    if line.startswith('#'):
        continue
    inp, _, fmt = line.partition('\t')
    out.append('{ memset(&tm,0,sizeof tm); in="%s"; r=strptime(in,"%s",&tm); if(!r) puts("null"); else printf("%%ld %%d %%d %%d %%d %%d %%d %%d %%d %%d %%ld\\n",(long)(r-in),tm.tm_sec,tm.tm_min,tm.tm_hour,tm.tm_mday,tm.tm_mon,tm.tm_year,tm.tm_wday,tm.tm_yday,tm.tm_isdst,(long)tm.tm_gmtoff); }' % (cesc(inp), cesc(fmt)))
out.append('return 0;}')
open(f'{w}/strptime.c', 'w').write('\n'.join(out))

# ---- tzset: one TZ per line ----
out = ['#include <stdio.h>', '#include <stdlib.h>', '#include <time.h>', 'int main(void){']
for line in cases(f'{d}/tzset.cases'):
    out.append(f'setenv("TZ","{line}",1); tzset(); printf("%s %s %ld %d\\n", tzname[0], tzname[1], (long)timezone, daylight);')
out.append('return 0;}')
open(f'{w}/tzset.c', 'w').write('\n'.join(out))
PY

docker run --rm -v "$work":/w golang:1.23 sh -c '
  gcc -w -o /printf /w/printf.c -lm && /printf > /w/printf.golden &&
  gcc -w -o /scan /w/scan.c && /scan > /w/scan.golden &&
  gcc -w -o /strftime /w/strftime.c && /strftime > /w/strftime.golden &&
  gcc -w -o /gmtime /w/gmtime.c && /gmtime > /w/gmtime.golden &&
  gcc -w -o /localtime /w/localtime.c && /localtime > /w/localtime.golden &&
  gcc -w -o /random /w/random.c && /random > /w/random.golden &&
  gcc -w -o /strptime /w/strptime.c && /strptime > /w/strptime.golden &&
  gcc -w -o /tzset /w/tzset.c && /tzset > /w/tzset.golden'
cp "$work/printf.golden" "$work/scan.golden" "$work/strftime.golden" "$work/gmtime.golden" "$work/localtime.golden" "$work/random.golden" "$work/strptime.golden" "$work/tzset.golden" "$dir/"
wc -l "$dir"/*.golden
