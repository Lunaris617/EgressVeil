#!/bin/sh
[ -f /etc/egressveil/activated ] || [ -f /var/run/egressveil.pending ] || exit 0
/usr/libexec/ua3f-integration/clash-hook
