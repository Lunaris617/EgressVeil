-- SPDX-License-Identifier: GPL-3.0-only
local fs=require "nixio.fs"
local sys=require "luci.sys"
local json=require "luci.jsonc"
local util=require "luci.util"
local m=Map("egressveil","EgressVeil","统一管理严格 HTTP UA、OpenClash/Mihomo 规则分流与 IPv4 TTL。未填写订阅时不接管网络；IPv6 路由、多 LAN 物理口及未知桥接结构会拒绝激活。")
local s=m:section(NamedSection,"main","suite","出口策略")
local enabled=s:option(Flag,"enabled","启用 EgressVeil")
enabled.rmempty=false
local feed=s:option(Value,"feed","节点订阅链接")
feed.password=true;feed.rmempty=true
function feed.validate(self,v)
 if not v or v=="" then return v end
 if #v>4096 or v:find("[%z\1-\32\127]") or not v:match("^https?://[^#]+$") then return nil,"请输入有效 HTTP(S) 订阅地址。" end
 return v
end
local selector=s:option(ListValue,"selector","代理节点／策略")
selector.default="自动选择";selector.rmempty=false
selector:value("自动选择","自动选择（延迟探测）")
local ok,nodes=pcall(json.parse,sys.exec("/usr/bin/ruby /usr/libexec/egressveil/config.rb selectors 2>/dev/null"))
if ok and type(nodes)=="table" and type(nodes.choices)=="table" then
 for _,v in ipairs(nodes.choices) do if v~="自动选择" then selector:value(v,v) end end
end
selector.description="首次激活使用自动选择；激活后刷新本页即可选择具体节点。国内直连，国际 UDP/QUIC 同样按规则与所选节点能力处理。"
local interval=s:option(Value,"interval","探测周期（秒）")
interval.default="30";interval.datatype="range(15,300)";interval.rmempty=false
local ua_enabled=s:option(Flag,"ua_enabled","改写明文 HTTP User-Agent")
ua_enabled.default="1";ua_enabled.rmempty=false
local ua=s:option(Value,"ua","统一 User-Agent")
ua.default="FFF";ua.rmempty=false
function ua.validate(self,v)
 if not v or #v==0 or #v>256 or v:find("[%z\1-\31\127]") then return nil,"UA 必须为 1–256 字节且不能包含控制字符。" end
 return v
end
local ttl=s:option(ListValue,"ttl","WAN 出站 IPv4 TTL")
ttl:value("64","64（严格配置，包含 GRO 防绕过保护）")
ttl.default="64";ttl.rmempty=false
function ttl.validate(self,v) if v~="64" then return nil,"当前严格配置只支持 TTL=64。" end return v end
ttl.description="TTL 由本套件独立维护。停用 HTTP UA 改写仍保留 TTL 和代理分流；关闭整个套件才撤销其接管。"
local lan=s:option(Value,"lan","LAN 逻辑接口")
lan.default="lan";lan.datatype="uciname";lan.rmempty=false
local wan=s:option(Value,"wan","WAN 逻辑接口")
wan.default="wan";wan.datatype="uciname";wan.rmempty=false
local level=s:option(ListValue,"log_level","核心日志级别")
for _,v in ipairs({"ERROR","WARN","INFO","DEBUG"}) do level:value(v) end
level.default="WARN";level.rmempty=false
local result=s:option(DummyValue,"_state","最近执行结果")
function result.cfgvalue() return fs.readfile("/var/run/egressveil/result") or "尚未激活；保存并应用后刷新本页。" end
local function running(name)
 local services=util.ubus("service","list",{name=name}) or {}
 for _,instance in pairs((services[name] or {}).instances or {}) do
  if instance.running==true and tonumber(instance.pid) then return true end
 end
 return false
end
local live=s:option(DummyValue,"_runtime","当前进程状态")
function live.cfgvalue()
 return "UA3F："..(running("ua3f") and "运行中" or "未运行").."；Mihomo："..(running("openclash") and "运行中" or "未运行")
end
local live_ttl=s:option(DummyValue,"_ttl_runtime","当前 TTL 出口规则")
function live_ttl.cfgvalue()
 return sys.call("nft list chain netdev ua3fi_ttl_last egress 2>/dev/null | grep -q 'ip ttl set 64'")==0 and "TTL=64 规则在位" or "规则未安装／未就绪"
end
local policy=s:option(DummyValue,"_policy","当前固定工作流")
function policy.cfgvalue() return "规则模式 · 国内直连 · 国际按节点选择 · fake-ip · HTTP/1/h2c 严格引擎 · HTTPS 不解密 · IPv4 TTL=64" end
function m.on_after_commit()
 sys.call("umask 077; mkdir -p /var/run/egressveil; chmod 700 /var/run/egressveil; (/usr/libexec/egressveil/setup apply >/var/run/egressveil/setup.log 2>&1) </dev/null &")
end
return m
