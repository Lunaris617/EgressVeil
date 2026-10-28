#!/usr/bin/ruby
# Patch only the reviewed official dockerd lifecycle structure. No daemon or
# Docker network/config changes. Fail before global bridge flags are lowered.
require 'digest'
path='/etc/init.d/dockerd'
begin
 text=File.read(path)
 if text.include?('# ua3fi-docker-lifecycle-v1')
  raise 'BAD_WRAPPER' unless text.scan('/usr/libexec/ua3f-bridge-policy/dockerd-launch /usr/bin/dockerd').length==2
  exit 0
 end
 raise 'UNKNOWN_DOCKER_INIT' unless text.scan('procd_set_param command /usr/bin/dockerd').length==2
 %w[start_service reload_service stop_service].each do |name|
  raise 'UNKNOWN_DOCKER_INIT' unless text.scan(name+"() {\n").length==1
 end
 # Existing custom wrappers must never be double-wrapped.
 raise 'EXISTING_WRAPPER' if text.include?('ua3f-bridge-policy')
 original=text.dup
 text.gsub!('procd_set_param command /usr/bin/dockerd','procd_set_param command /usr/libexec/ua3f-bridge-policy/dockerd-launch /usr/bin/dockerd')
 {'start_service'=>'dockerd-start','reload_service'=>'dockerd-reload','stop_service'=>'dockerd-stop'}.each do |name,reason|
  text.sub!(name+"() {\n",name+"() {\n\t/usr/libexec/ua3f-bridge-policy/bridge-policy barrier #{reason} || return 1\n")
 end
 text.sub!("\tprocd_send_signal dockerd\n", "\tprocd_send_signal dockerd\n\t/usr/libexec/ua3f-bridge-policy/bridge-policy schedule-current || :\n")
 text.sub!("\t\tservice_stop \"/usr/bin/dockerd\"\n\tfi\n}", "\t\tservice_stop \"/usr/bin/dockerd\"\n\tfi\n\t/usr/libexec/ua3f-bridge-policy/bridge-policy schedule-stopped || :\n}")
 raise 'STOP_HOOK_MISSING' unless text.include?('schedule-stopped')
 text="#{text.lines.first}# ua3fi-docker-lifecycle-v1\n"+text.lines.drop(1).join
 backup='/etc/egressveil/docker-init.original'
 File.open(backup,File::WRONLY|File::CREAT|File::EXCL,0600){|f|f.write(original)} unless File.exist?(backup)
 tmp=path+'.ua3fi-new'
 File.open(tmp,File::WRONLY|File::CREAT|File::EXCL,0755){|f|f.write(text);f.flush;f.fsync}
 raise 'SYNTAX' unless system('/bin/sh','-n',tmp)
 File.rename(tmp,path)
 puts 'DOCKER_LIFECYCLE_READY'
rescue StandardError
 STDERR.puts 'DOCKER_INIT_REQUIRES_REVIEW'
 exit 1
ensure
 File.unlink(tmp) if defined?(tmp) && tmp && File.exist?(tmp)
end
