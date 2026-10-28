#!/usr/bin/ruby
# No configuration values, node names, addresses or credentials are printed.
require 'yaml'
require 'digest'

def fail_code(code)
  STDERR.puts code
  exit 1
end

def direct_resolvers(values)
  Array(values).map do |v|
    s = String(v).sub(/#DIRECT\z/, '')
    s = "udp://#{s}" unless s.include?('://')
    fail_code('BOOTSTRAP_REVIEW_REQUIRED') unless s.match?(/\A(?:udp|tcp|tls|https):\/\/[0-9.]+(?::[0-9]+)?(?:\/[^#\s]*)?\z/)
    "#{s}#DIRECT"
  end
end

begin
  op, input, output = ARGV
  fail_code('ARGUMENTS_INVALID') unless %w[stage apply].include?(op) && input && (op == 'apply' || output)
  fail_code('INPUT_SYMLINK_REJECTED') if File.symlink?(input)
  cfg = YAML.load_file(input)
  fail_code('CONFIG_MAPPING_REQUIRED') unless cfg.is_a?(Hash) && cfg['dns'].is_a?(Hash)
  before = Marshal.load(Marshal.dump(cfg))
  groups = Array(cfg['proxy-groups']).select { |g| g.is_a?(Hash) && g['type'] == 'url-test' }
  fail_code('UNIQUE_URLTEST_REQUIRED') unless groups.length == 1
  group = groups.first
  name = group['name']
  fail_code('GROUP_REVIEW_REQUIRED') unless name.is_a?(String) && !name.empty? && !name.match?(/[\x00-\x1f&#=]/) && !Array(group['proxies']).include?('DIRECT')
  # This group also requires a successful live API no-DIRECT audit before first deployment.
  dns = cfg['dns']
  bootstrap = direct_resolvers(dns['proxy-server-nameserver'])
  fail_code('BOOTSTRAP_REQUIRED') if bootstrap.empty?
  policy = { 'geosite:cn' => bootstrap.dup }
  existing = dns['nameserver-policy']
  fail_code('EXISTING_POLICY_REVIEW_REQUIRED') unless existing.nil? || existing.empty? || existing == policy
  fail_code('BOOTSTRAP_POLICY_REVIEW_REQUIRED') unless (dns['proxy-server-nameserver-policy'] || {}).empty?
  foreign = ["https://1.1.1.1/dns-query##{name}", "https://8.8.8.8/dns-query##{name}"]
  dns['nameserver'] = foreign.dup
  dns['default-nameserver'] = bootstrap.dup
  dns['proxy-server-nameserver'] = bootstrap.dup
  dns['nameserver-policy'] = policy
  dns['direct-nameserver'] = foreign.dup
  dns['direct-nameserver-follow-policy'] = true
  dns['respect-rules'] = false
  dns['prefer-h3'] = false
  %w[fallback fallback-filter fallback-lazy-query].each { |k| dns.delete(k) }
  fail_code('NON_DNS_CHANGE_REJECTED') unless before.reject { |k, _| k == 'dns' } == cfg.reject { |k, _| k == 'dns' }
  target = op == 'apply' ? input : output
  fail_code('STAGE_MUST_DIFFER') if op == 'stage' && File.expand_path(input) == File.expand_path(output)
  fail_code('TARGET_SYMLINK_REJECTED') if File.symlink?(target)
  tmp = "#{target}.ua3fi-dns-#{Process.pid}"
  File.open(tmp, File::WRONLY | File::CREAT | File::EXCL, 0600) { |f| f.write(YAML.dump(cfg)); f.flush; f.fsync }
  roundtrip = YAML.load_file(tmp)
  fail_code('ROUNDTRIP_MISMATCH') unless roundtrip == cfg
  File.rename(tmp, target)
  puts 'DNS_ONLY_POLICY_WRITTEN'
rescue StandardError => error
  kind = case error
         when NoMethodError then 'METHOD'
         when NameError then 'CONSTANT'
         when TypeError then 'TYPE'
         when ArgumentError then 'ARGUMENT'
         when SystemCallError then 'FILESYSTEM'
         else 'OTHER'
         end
  line = Array(error.backtrace_locations).find { |x| x.path == __FILE__ }
  STDERR.puts "DNS_POLICY_FAILED_#{kind}_LINE_#{line ? line.lineno : 0}"
  exit 1
ensure
  File.unlink(tmp) if defined?(tmp) && tmp && File.exist?(tmp)
end
