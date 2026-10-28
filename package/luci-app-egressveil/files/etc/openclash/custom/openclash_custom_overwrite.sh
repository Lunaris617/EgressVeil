#!/bin/sh
[ -f /etc/egressveil/activated ] || [ -f /var/run/egressveil.pending ] || exit 0
ruby /usr/libexec/egressveil/config.rb normalize-dns "$1" || exit 1
ruby /usr/libexec/ua3f-integration/dns-policy.rb apply "$1" || exit 1
ruby /usr/libexec/ua3f-integration/udp-target-policy.rb apply "$1" || exit 1
