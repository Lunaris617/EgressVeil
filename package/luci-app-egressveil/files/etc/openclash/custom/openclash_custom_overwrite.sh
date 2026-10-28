#!/bin/sh
[ -f /etc/egressveil/activated ] || [ -f /var/run/egressveil.pending ] || exit 0
exec ruby /usr/libexec/egressveil/config.rb normalize-dns "$1"
