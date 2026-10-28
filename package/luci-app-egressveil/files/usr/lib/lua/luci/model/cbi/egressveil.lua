-- SPDX-License-Identifier: GPL-3.0-only
local fs=require "nixio.fs"
local sys=require "luci.sys"
local util=require "luci.util"
local uci=require("luci.model.uci").cursor()
local m=Map("egressveil","EgressVeil","按需选择代理、明文 HTTP UA 和 IPv4 TTL。流量接管由 EgressVeil 自动配置。首次使用请完成三项选择；没有订阅也可使用。")
m:append(Template("egressveil/style"))
local s=m:section(NamedSection,"main","suite","配置引导与出口设置")
local enabled=s:option(Flag,"enabled","运行 EgressVeil")
enabled.rmempty=false
local function choice(key,title,yes,no)
 local o=s:option(ListValue,key,title)
 o:value("","请选择");o:value("0",no);o:value("1",yes);o.rmempty=false
 function o.validate(self,v) if v~="0" and v~="1" then return nil,"请明确选择开启或关闭。" end return v end
 return o
end
choice("proxy_enabled","代理","使用代理","暂不配置，全部直连")
choice("ua_enabled","明文 HTTP UA","开启改写","保留原始 UA")
choice("ttl_enabled","IPv4 TTL","开启改写","保留原始 TTL")
local feed=s:option(Value,"feed","节点订阅链接")
feed.password=true;feed.rmempty=true
feed.description="可留空或后续更换。快速订阅每小时刷新；完整配置订阅可在订阅管理中导入、手动或定时更新。"
function feed.validate(self,v)
 if not v or v=="" then return "" end
 if #v>4096 or v:find("[%z\1-\32\127]") or not v:match("^https?://[^#]+$") then return nil,"请输入 HTTP(S) 订阅地址。" end
 return v
end
local refresh=s:option(Button,"_refresh_feed","快速订阅")
refresh.inputtitle="立即更新";refresh.inputstyle="apply"
function refresh.write()
 local code=sys.call("ruby /usr/libexec/egressveil/config.rb refresh >/dev/null 2>&1")
 m.message=code==0 and "快速订阅已更新。" or "当前核心未就绪或未使用快速订阅，请在订阅管理或 Metacubexd 中更新相应提供商。"
end
local profile=s:option(ListValue,"proxy_config","代理配置文件")
profile:value("","未选择（填写订阅后自动生成）")
for p in fs.glob("/etc/openclash/config/*") do
 if (p:match("%.yaml$") or p:match("%.yml$")) and not p:match("egressveil%-direct%.yaml$") then profile:value(p,fs.basename(p)) end
end
profile.rmempty=true
local mode=s:option(ListValue,"proxy_mode","代理策略")
mode:value("rule","规则");mode:value("global","全局");mode.default="rule";mode.rmempty=false
mode.description="代理关闭时全部直连；开启后使用此策略。策略组和具体节点在 Metacubexd 中选择。"
local ua=s:option(Value,"ua","统一 User-Agent")
ua.default="FFF";ua.rmempty=false
function ua.validate(self,v)
 if not v or #v==0 or #v>256 or v:find("[%z\1-\31\127]") then return nil,"请输入 1–256 字节的 UA，不支持控制字符。" end return v
end
ua.description="覆盖 HTTP/1 与明文 HTTP/2（h2c）。HTTPS 不解密；正则改写不在首版支持范围内。"
local ttl=s:option(Value,"ttl","WAN 出站 IPv4 TTL")
ttl.default="64";ttl.datatype="range(1,255)";ttl.rmempty=false
ttl.description="仅在 TTL 改写开启时生效。IPv6 不属于首版支持范围。"
local interval=s:option(Value,"interval","自动选择探测间隔（秒）")
interval.description="用于快速订阅生成的自动选择组；导入配置的策略组由原配置管理。"
interval.default="30";interval.datatype="range(15,300)";interval.rmempty=false
local lan=s:option(Value,"lan","LAN 逻辑接口")
lan.default="lan";lan.datatype="uciname";lan.rmempty=false
local wan=s:option(Value,"wan","WAN 逻辑接口")
wan.default="wan";wan.datatype="uciname";wan.rmempty=false
local level=s:option(ListValue,"log_level","UA3F 日志级别")
for _,v in ipairs({"ERROR","WARN","INFO","DEBUG"}) do level:value(v) end
level.default="WARN";level.rmempty=false
local function running(name)
 local services=util.ubus("service","list",{name=name}) or {}
 for _,instance in pairs((services[name] or {}).instances or {}) do if instance.running==true and tonumber(instance.pid) then return true end end
 return false
end
local state=s:option(DummyValue,"_state","应用结果")
function state.cfgvalue() return fs.readfile("/var/run/egressveil/result") or "尚未应用" end
local health=s:option(DummyValue,"_health","当前就绪状态")
function health.cfgvalue()
 if uci:get("egressveil","main","enabled")~="1" then return "已暂停" end
 if sys.call("ruby /usr/libexec/egressveil/config.rb health --quick >/dev/null 2>&1")~=0 then return "核心未就绪，请检查应用结果与日志" end
 if uci:get("egressveil","main","ua_enabled")=="1" and sys.call("/usr/libexec/ua3f-integration/wait-ready --check >/dev/null 2>&1")~=0 then return "UA 接管未就绪，保护保留" end
 if uci:get("egressveil","main","ttl_enabled")=="1" and sys.call("/usr/libexec/ua3f-integration/ttl-policy check >/dev/null 2>&1")~=0 then return "TTL 保护未就绪" end
 return "接管已就绪"
end
local live=s:option(DummyValue,"_runtime","实际进程")
function live.cfgvalue() return "Mihomo："..(running("openclash") and "运行中" or "未运行").."；UA3F："..(running("ua3f") and "运行中" or "未运行").."；控制入口："..(running("egressveil-controller") and "运行中" or "未运行") end
local rule=s:option(DummyValue,"_ttl_runtime","实际 TTL 规则")
function rule.cfgvalue()
 local n=tonumber(uci:get("egressveil","main","ttl"))
 if not n then return "未配置" end
 return sys.call("nft list chain netdev ua3fi_ttl_last egress 2>/dev/null | grep -q 'ip ttl set "..n.." '")==0 and "TTL="..n.." 规则已安装" or "未安装"
end
local apply_generation
function m.on_before_commit(self)
 self.uci:set("egressveil","main","configured","1")
 apply_generation=(fs.readfile("/proc/sys/kernel/random/uuid") or ""):match("^([a-f0-9-]+)")
 if apply_generation then self.uci:set("egressveil","main","apply_generation",apply_generation) end
end
function m.on_after_commit()
 if not apply_generation then return end
 sys.call("umask 077; mkdir -p /var/run/egressveil; chmod 700 /var/run/egressveil; (/usr/libexec/egressveil/ui-apply "..apply_generation.." >/var/run/egressveil/setup.log 2>&1) </dev/null &")
end
return m
