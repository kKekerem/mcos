#!/bin/sh
guard() {
    ( exec <"$1" >"$1" ) 2>/dev/null || { echo "guard: cannot use $1"; return 0; }
    echo "guard: would use $1"
}
guard /definitely/not/here
echo "parent still alive: $?"
guard /dev/null
echo "after: $?"
