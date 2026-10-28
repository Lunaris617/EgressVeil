#!/usr/bin/ruby
require 'yaml'
require 'json'

ROOT='/etc/egressveil'
SOURCE='/etc/openclash/config/ua3fi.yaml'
def value(key)
 IO.popen(['uci','-q','get','egressveil.main.'+key]){|io|io.read.strip}
end
def atomic(path,text)
 dir=File.dirname(path);Dir.mkdir(dir,0700) unless File.directory?(dir)
 tmp=path+'.suite-'+Process.pid.to_s
 File.open(tmp,File::WRONLY|File::CREAT|File::EXCL,0600){|f|f.write(text);f.flush;f.fsync}
 File.rename(tmp,path)
ensure
 File.unlink(tmp) if tmp && File.exist?(tmp)
end
def secret(name)
 file=ROOT+'/'+name
 return File.read(file).strip if File.file?(file) && !File.symlink?(file)
 text=File.read('/proc/sys/kernel/random/uuid').strip.delete('-')
 atomic(file,text+"\n");text
end
def api(path,method='GET',body=nil)
 cfg=YAML.load_file(SOURCE)
 key=cfg.fetch('secret')
 spec="silent\nshow-error\nmax-time=20\nrequest=#{method}\nurl=\"http://127.0.0.1:9090#{path}\"\nheader="+('Authorization: Bearer '+key).dump+"\nwrite-out=\"\\n%{http_code}\"\n"
 if body
  spec += 'header="Content-Type: application/json"'+"\n"+'data='+JSON.generate(JSON.generate(body))+"\n"
 end
 raw=IO.popen(['curl','--noproxy','*','-K','-'],'r+'){|io|io.write(spec);io.close_write;io.read}
 body,_,code=raw.rpartition("\n")
 raise 'API_FAILED' unless $?.success? && code.to_i.between?(200,299)
 body.empty? ? {} : YAML.load(body)
end
begin
 case ARGV[0]
 when 'selectors'
  g=api('/proxies/Switch')
  puts JSON.generate({'current'=>g['now'],'choices'=>g['all']})
 when 'select'
  selected=value('selector');selected='自动选择' if selected.empty?
  raise 'SELECTOR_INVALID' unless Array(api('/proxies/Switch')['all']).include?(selected)
  api('/proxies/Switch','PUT',{'name'=>selected})
  puts 'SELECTOR_APPLIED'
 when 'validate' 
  feed=value('feed')
  raise 'FEED_INVALID' unless feed.bytesize.between?(10,4096) && feed.match?(/\Ahttps?:\/\/[^\s\x00-\x1f#]+\z/)
  raise 'TTL_PROFILE_INVALID' unless value('ttl')=='64'
  raise 'UA_SWITCH_INVALID' unless %w[0 1].include?(value('ua_enabled'))
  ua=value('ua');raise 'UA_INVALID' unless ua.bytesize.between?(1,256) && !ua.match?(/[\x00-\x1f\x7f]/)
  raise 'LOG_LEVEL_INVALID' unless %w[DEBUG INFO WARN ERROR].include?(value('log_level'))
  raise 'INTERVAL_INVALID' unless value('interval').match?(/\A[0-9]+\z/) && value('interval').to_i.between?(15,300)
  puts 'PROFILE_VALID' 
 when 'generate'
  feed=value('feed')
  raise 'FEED_INVALID' unless feed.bytesize.between?(10,4096) && feed.match?(/\Ahttps?:\/\/[^\s\x00-\x1f#]+\z/)
  cfg={
   'mixed-port'=>7893,'port'=>7890,'socks-port'=>7891,'redir-port'=>7892,'tproxy-port'=>7895,
   'allow-lan'=>true,'bind-address'=>'*','mode'=>'rule','log-level'=>'warning','ipv6'=>false,
   'external-controller'=>'0.0.0.0:9090','secret'=>secret('controller.key'),
   'authentication'=>['ua3fi:'+secret('proxy.key')],
   'profile'=>{'store-selected'=>true},'geodata-mode'=>false,
   'dns'=>{'enable'=>true,'listen'=>'0.0.0.0:7874','ipv6'=>false,'enhanced-mode'=>'fake-ip',
      'fake-ip-range'=>'198.18.0.1/16','use-hosts'=>true,
      'fake-ip-filter'=>['*.lan','*.local','localhost','+.pool.ntp.org','+.time.windows.com'],
      'proxy-server-nameserver'=>['udp://223.5.5.5#DIRECT','udp://119.29.29.29#DIRECT']},
   'proxy-providers'=>{'Global-ISP'=>{'type'=>'http','url'=>feed,
      'path'=>'./proxy_provider/Global-ISP.yaml','interval'=>3600,'proxy'=>'DIRECT',
      'header'=>{'User-Agent'=>['curl']},
      'exclude-filter'=>'剩余流量|到期时间|套餐信息|重置时间|欢迎使用|共∞|本代理',
      'health-check'=>{'enable'=>true,'url'=>'http://www.gstatic.com/generate_204','interval'=>value('interval').to_i,'lazy'=>false}}},
   'proxy-groups'=>[
      {'name'=>'Switch','type'=>'select','proxies'=>['自动选择'],'use'=>['Global-ISP'],'default-selected'=>'自动选择'},
      {'name'=>'自动选择','type'=>'url-test','use'=>['Global-ISP'],'url'=>'http://www.gstatic.com/generate_204','interval'=>value('interval').to_i,'tolerance'=>100,'lazy'=>false},
      {'name'=>'CN','type'=>'select','proxies'=>['DIRECT']}],
   'rules'=>['DOMAIN-KEYWORD,microsoft,DIRECT','GEOIP,LAN,DIRECT','GEOSITE,cn,CN','GEOIP,CN,CN,no-resolve','MATCH,Switch']
  }
  atomic(SOURCE,YAML.dump(cfg))
  raise 'DNS_SETUP_FAILED' unless system('ruby','/usr/libexec/ua3f-integration/dns-policy.rb','apply',SOURCE,out:File::NULL,err:File::NULL)
  puts 'CONFIG_GENERATED'
 when 'normalize-dns'
  target=ARGV[1]
  allowed=['/tmp/ua3fi.yaml','/etc/openclash/ua3fi.yaml']
  raise 'TARGET_INVALID' unless target && allowed.include?(File.expand_path(target)) && !File.symlink?(target)
  st=File.stat(target)
  raise 'TARGET_OWNERSHIP_INVALID' unless st.file? && st.uid==0 && (st.mode & 0022)==0
  cfg=YAML.load_file(target)
  # EgressVeil owns the frontend. Do not download a second dashboard at boot.
  %w[external-ui external-ui-name external-ui-url].each{|k|cfg.delete(k)}
  original=YAML.load_file(SOURCE).fetch('dns')
  %w[proxy-server-nameserver nameserver-policy].each{|k|cfg.fetch('dns')[k]=original.fetch(k)}
  cfg['dns'].delete('proxy-server-nameserver-policy')
  atomic(target,YAML.dump(cfg))
 when 'health'
  source=YAML.load_file(SOURCE)
  generated=YAML.load_file('/etc/openclash/ua3fi.yaml')
  %w[nameserver default-nameserver proxy-server-nameserver nameserver-policy direct-nameserver direct-nameserver-follow-policy respect-rules ipv6].each do |k|
   raise 'GENERATED_DNS_MISMATCH' unless generated.fetch('dns')[k]==source.fetch('dns')[k]
  end
  raise 'SECOND_DASHBOARD_ENABLED' if generated.keys.any?{|k|k.start_with?('external-ui')}
  api('/providers/proxies/Global-ISP/healthcheck')
  sleep 1
  data=api('/providers/proxies')['providers'].fetch('Global-ISP')
  nodes=data.fetch('proxies')
  raise 'NO_HEALTHY_NODE' unless nodes.any?{|p|p['alive'] && (p['history']||[]).any?{|h|h['delay'].to_i>0}}
  raise 'MODE_NOT_RULE' unless api('/configs')['mode']=='rule'
  puts 'CORE_HEALTHY_RULE_MODE'
 when 'status'
  ready=File.file?(ROOT+'/activated')
  puts(ready ? 'SUITE_ACTIVATED' : 'SUITE_NOT_ACTIVATED')
 else
  raise 'OPERATION_INVALID'
 end
rescue StandardError
 STDERR.puts 'SUITE_CONFIG_FAILED'
 exit 1
end
