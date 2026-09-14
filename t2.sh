#!/bin/sh
# read -t fallback: EOF ve desteklenmeyen -t durumlarinda akis devam etmeli
printf 'choice prompt: '
if read -t 5 choice 2>/dev/null; then
    echo "got=[$choice]"
else
    choice=1
    echo "fallback choice=$choice"
fi
[ -z "$choice" ] && choice=1
echo "final=$choice"
printf 'untimed-equivalent: '
read -t 30 _ 2>/dev/null || true
echo "continued past read, rc=$?"
