#!/usr/bin/ruby
# SPDX-License-Identifier: GPL-3.0-only
require 'yaml'
require 'json'
ROOT='/etc/egressveil'
SOURCE='/etc/openclash/config/ua3fi.yaml'
DIRECT='/etc/openclash/config/egressveil-direct.yaml'
def value(key, default='')
 s=IO.popen(['uci','-q','get','egressveil.main.'+key]){|io|io.read.strip}
 s.empty? ? default : s
end
def oc(key)
 IO.popen(['uci','-q','get','openclash.config.'+key]){|io|io.read.strip}
end
def atomic(path,text)
 dir=File.dirname(path);Dir.mkdir(dir,0700) unless File.directory?(dir)
 raise 'SYMLINK_REJECTED' if File.symlink?(path)
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
 key=File.read(ROOT+'/controller.key').strip
 timeout=ARGV.include?('--quick') ? 2 : 20
 spec="silent\nshow-error\nmax-time=#{timeout}\nrequest=#{method}\nurl=\"http://127.0.0.1:9091#{path}\"\nheader="+('Authorization: Bearer '+key).dump+"\nwrite-out=\"\\n%{http_code}\"\n"
 spec+='header="Content-Type: application/json"'+"\n"+'data='+JSON.generate(JSON.generate(body))+"\n" if body
 raw=IO.popen(['curl','--noproxy','*','-K','-'],'r+'){|io|io.write(spec);io.close_write;io.read}
 body,_,code=raw.rpartition("\n")
 raise 'API_FAILED' unless $?.success? && code.to_i.between?(200,299)
 body.empty? ? {} : JSON.parse(body)
end
def valid_feed?(feed)
 feed.bytesize.between?(10,4096) && feed.match?(/\Ahttps?:\/\/[^\s\x00-\x1f#]+\z/)
end
def selected_source
 p=oc('config_path')
 raise 'SOURCE_PATH_INVALID' unless p.match?(%r{\A/etc/openclash/config/[A-Za-z0-9_. -]+\.ya?ml\z}) && !File.symlink?(p) && File.file?(p)
 p
end
def base
 {'mixed-port'=>7893,'port'=>7890,'socks-port'=>7891,'redir-port'=>7892,'tproxy-port'=>7895,
 'allow-lan'=>true,'bind-address'=>'*','mode'=>'rule','log-level'=>'warning','ipv6'=>false,
 'external-controller'=>'127.0.0.1:9091','secret'=>secret('controller.key'),
 'external-ui'=>'/usr/share/openclash/ui',
 'authentication'=>['egressveil:'+secret('proxy.key')], 'profile'=>{'store-selected'=>true},'geodata-mode'=>false,
 'dns'=>{'enable'=>true,'listen'=>'0.0.0.0:7874','ipv6'=>false,'enhanced-mode'=>'fake-ip',
 'fake-ip-range'=>'198.18.0.1/16','use-hosts'=>true,
 'fake-ip-filter'=>['*.lan','*.local','localhost','+.pool.ntp.org','+.time.windows.com'],
 'proxy-server-nameserver'=>['udp://223.5.5.5#DIRECT','udp://119.29.29.29#DIRECT']}}
end
def managed_dns(c, direct=false)
 d=c.fetch('dns');bootstrap=['udp://223.5.5.5#DIRECT','udp://119.29.29.29#DIRECT']
 foreign=direct ? bootstrap : ['https://1.1.1.1/dns-query#自动选择','https://8.8.8.8/dns-query#自动选择']
 d.merge!('nameserver'=>foreign,'default-nameserver'=>bootstrap,'proxy-server-nameserver'=>bootstrap,
 'nameserver-policy'=>{'geosite:cn'=>bootstrap},'direct-nameserver'=>foreign,
 'direct-nameserver-follow-policy'=>true,'respect-rules'=>false,'prefer-h3'=>false)
end
begin
 case ARGV[0]
 when 'runtime-contract'
  checks={'en_mode'=>'fake-ip','operation_mode'=>'fake-ip','china_ip_route'=>'0','router_self_proxy'=>'1','disable_udp_quic'=>'0','ipv6_enable'=>'0','ipv6_dns'=>'0','cn_port'=>'9090',
   'proxy_port'=>'7892','tproxy_port'=>'7895','mixed_port'=>'7893','socks_port'=>'7891','http_port'=>'7890','dns_port'=>'7874'}
  checks.each{|k,v|raise 'INCOMPATIBLE_OPENCLASH_SETTINGS' unless oc(k)==v}
  puts 'RUNTIME_CONTRACT_VALID'
 when 'validate'
  %w[proxy_enabled ua_enabled ttl_enabled].each{|k|raise 'SWITCH_INVALID' unless %w[0 1].include?(value(k))}
  raise 'WIZARD_INCOMPLETE' unless value('configured')=='1'
  feed=value('feed');raise 'FEED_INVALID' unless feed.empty? || valid_feed?(feed)
  if value('proxy_enabled')=='1' && feed.empty?
   p=value('proxy_config');p=oc('config_path') if p.empty?
   raise 'PROXY_CONFIG_REQUIRED' unless p!=DIRECT && File.file?(p) && !File.symlink?(p)
  end
  raise 'TTL_INVALID' unless value('ttl').match?(/\A[0-9]{1,3}\z/) && value('ttl').to_i.between?(1,255)
  ua=value('ua');raise 'UA_INVALID' unless ua.bytesize.between?(1,256) && !ua.match?(/[\x00-\x1f\x7f]/)
  raise 'LOG_LEVEL_INVALID' unless %w[DEBUG INFO WARN ERROR].include?(value('log_level'))
  raise 'INTERVAL_INVALID' unless value('interval').match?(/\A[0-9]+\z/) && value('interval').to_i.between?(15,300)
  puts 'PROFILE_VALID'
 when 'generate'
  secret('controller.key');secret('proxy.key')
  direct=base;direct['mode']='direct';direct['proxies']=[];direct['proxy-groups']=[];direct['rules']=['MATCH,DIRECT'];managed_dns(direct,true)
  atomic(DIRECT,YAML.dump(direct))
  feed=value('feed')
  if !feed.empty? && (!File.file?(SOURCE) || !File.file?(ROOT+'/last-feed') || File.read(ROOT+'/last-feed').strip!=feed)
   raise 'FEED_INVALID' unless valid_feed?(feed)
   c=base
   c['proxy-providers']={'Global-ISP'=>{'type'=>'http','url'=>feed,'path'=>'./proxy_provider/Global-ISP.yaml','interval'=>3600,'proxy'=>'DIRECT',
    'header'=>{'User-Agent'=>['curl']},'exclude-filter'=>'剩余流量|到期时间|套餐信息|重置时间|欢迎使用|共∞|本代理',
    'health-check'=>{'enable'=>true,'url'=>'http://www.gstatic.com/generate_204','interval'=>value('interval','30').to_i,'lazy'=>false}}}
   c['proxy-groups']=[{'name'=>'Switch','type'=>'select','proxies'=>['自动选择'],'use'=>['Global-ISP']},
    {'name'=>'自动选择','type'=>'url-test','use'=>['Global-ISP'],'url'=>'http://www.gstatic.com/generate_204','interval'=>value('interval','30').to_i,'tolerance'=>100,'lazy'=>false},
    {'name'=>'CN','type'=>'select','proxies'=>['DIRECT']}]
   c['rules']=['DOMAIN-KEYWORD,microsoft,DIRECT','GEOIP,LAN,DIRECT','GEOSITE,cn,CN','GEOIP,CN,CN,no-resolve','MATCH,Switch']
   managed_dns(c);atomic(SOURCE,YAML.dump(c))
  end
  if File.file?(SOURCE)
   c=YAML.load_file(SOURCE,aliases:true);before=Marshal.load(Marshal.dump(c))
   provider=(c['proxy-providers']||{})['Global-ISP']
   provider['health-check']['interval']=value('interval','30').to_i if provider.is_a?(Hash) && provider['health-check'].is_a?(Hash)
   Array(c['proxy-groups']).each{|g|g['interval']=value('interval','30').to_i if g['name']=='自动选择' && g['type']=='url-test'}
   atomic(SOURCE,YAML.dump(c)) unless c==before
  end
  puts 'CONFIG_PREPARED'
 when 'normalize-dns'
  source=selected_source;name=File.basename(source);target=ARGV[1]
  allowed=['/tmp/'+name,'/etc/openclash/'+name]
  raise 'TARGET_INVALID' unless target && allowed.include?(File.expand_path(target)) && !File.symlink?(target)
  st=File.stat(target);raise 'TARGET_OWNERSHIP_INVALID' unless st.file? && st.uid==0 && (st.mode & 0022)==0
  c=YAML.load_file(target, aliases: true);original=YAML.load_file(source, aliases: true)
  raise 'CONFIG_MAPPING_REQUIRED' unless c.is_a?(Hash) && original.is_a?(Hash)
  c.merge!('port'=>7890,'socks-port'=>7891,'mixed-port'=>7893,'redir-port'=>7892,'tproxy-port'=>7895,
   'external-controller'=>'127.0.0.1:9091','secret'=>secret('controller.key'),'external-ui'=>'/usr/share/openclash/ui',
   'ipv6'=>false,'mode'=>value('proxy_enabled')=='1' ? value('proxy_mode','rule') : 'direct')
  %w[external-ui-name external-ui-url external-controller-tls external-controller-unix external-controller-pipe external-doh-server].each{|k|c.delete(k)}
  c['tun']={'enable'=>false}
  c['dns']||={}
  c['dns'].merge!('enable'=>true,'listen'=>'0.0.0.0:7874','ipv6'=>false,'enhanced-mode'=>'fake-ip','fake-ip-range'=>'198.18.0.1/16')
  if original['dns'].is_a?(Hash) && oc('enable_custom_dns')!='1'
   original['dns'].each{|k,v|c['dns'][k]=v unless %w[enable listen ipv6 enhanced-mode fake-ip-range].include?(k)}
  end
  if value('proxy_enabled')!='1'
   c['proxies']=[];c['proxy-groups']=[];c['proxy-providers']={};c['rules']=['MATCH,DIRECT']
   if oc('enable_custom_dns')=='1'
    check_dns=lambda do |v|
     case v
     when Hash then v.each_value{|x|check_dns.call(x)}
     when Array then v.each{|x|check_dns.call(x)}
     when String
      suffix=v.split('#',2)[1]
      raise 'DNS_PROXY_NOT_ALLOWED_WHEN_DIRECT' if suffix && !%w[DIRECT h3].include?(suffix)
     end
    end
    check_dns.call(c['dns'])
   else
    managed_dns(c,true)
   end
  end
  raise 'DNS_NAMESERVER_REQUIRED'   if Array(c['dns']['nameserver']).empty?
  atomic(target,YAML.dump(c))
 when 'health'
  c=YAML.load_file('/etc/openclash/'+File.basename(selected_source), aliases: true)
  raise 'BACKEND_EXPOSED' unless c['external-controller']=='127.0.0.1:9091'
  expected=value('proxy_enabled')=='1' ? value('proxy_mode','rule') : 'direct'
  raise 'MODE_MISMATCH' unless api('/configs')['mode']==expected
  api('/version');puts 'CORE_READY'
 when 'refresh'
  api('/providers/proxies/Global-ISP','PUT');puts 'SUBSCRIPTION_REFRESHED'
 when 'selectors'
  puts JSON.generate(api('/proxies')['proxies'].select{|_,g|g['type']=='Selector'})
 when 'select'
  puts 'SELECTION_PRESERVED'
 when 'status'
  puts(File.file?(ROOT+'/activated') ? 'SUITE_ACTIVATED' : 'SUITE_NOT_ACTIVATED')
 else
  raise 'OPERATION_INVALID'
 end
rescue StandardError=>e
 code=e.message.match?(/\A[A-Z_]+\z/) ? e.message : 'CONFIGURATION_REJECTED'
 line=Array(e.backtrace_locations).find{|x|x.path==__FILE__}; STDERR.puts(code+'_LINE_'+(line ? line.lineno.to_s : '0')+'_'+e.class.name);exit 1
end
