#!/usr/bin/ruby
# SPDX-License-Identifier: GPL-3.0-only
require 'json'
def value(k)
  IO.popen(['uci','-q','get','egressveil.main.'+k]){|io|io.read.strip}
end
def check(*args)
  system(*args,out:'/dev/null',err:'/dev/null')
end
health='接管已就绪'
if value('enabled')!='1'
  health='已暂停'
elsif !check('ruby','/usr/libexec/egressveil/config.rb','health','--quick')
  health='核心未就绪，请检查应用结果与日志'
elsif value('ua_enabled')=='1' && !check('/usr/libexec/ua3f-integration/wait-ready','--check')
  health='UA 接管未就绪，保护保留'
elsif value('ttl_enabled')=='1' && !check('/usr/libexec/ua3f-integration/ttl-policy','check')
  health='TTL 保护未就绪'
end
path='/var/run/egressveil/ui-status.json'
tmp=path+'.'+Process.pid.to_s
File.open(tmp,File::WRONLY|File::CREAT|File::EXCL,0600){|f|f.write(JSON.generate({'health'=>health,'checked_at'=>Time.now.to_i}));f.flush;f.fsync}
File.rename(tmp,path)
