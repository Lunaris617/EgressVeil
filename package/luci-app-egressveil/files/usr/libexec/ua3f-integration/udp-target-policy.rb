#!/usr/bin/ruby
require 'yaml'
abort 'ARGUMENTS_INVALID' unless ARGV.size==2 && ARGV[0]=='apply'
p=ARGV[1];abort 'INPUT_REJECTED' unless File.file?(p) && !File.symlink?(p)
c=YAML.load_file(p);c['rules']=c.fetch('rules').reject{|r|r=='NETWORK,udp,DIRECT'}
abort 'PROXY_DEFAULT_REQUIRED' unless c['rules'].last=='MATCH,Switch'
t=p+'.ua3fi-'+Process.pid.to_s
begin
 File.open(t,File::WRONLY|File::CREAT|File::EXCL,0600){|f|f.write(YAML.dump(c));f.flush;f.fsync}
 File.rename(t,p)
ensure
 File.unlink(t) if File.exist?(t)
end
