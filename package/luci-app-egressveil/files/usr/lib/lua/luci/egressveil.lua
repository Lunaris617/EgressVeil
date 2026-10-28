
-- SPDX-License-Identifier: GPL-3.0-only
local M={}
function M.form(m)
 local owned={en_mode=true,stack_type=true,proxy_mode=true,enable_udp_proxy=true,
 router_self_proxy=true,china_ip_route=true,china_ip6_route=true,disable_udp_quic=true,
 common_ports=true,bypass_gateway_compatible=true,lan_ac_mode=true,
 ipv6_enable=true,ipv6_dns=true,cn_port=true,proxy_port=true,tproxy_port=true,
 mixed_port=true,socks_port=true,http_port=true,dns_port=true,interface_name=true,
 firewall_custom=true,small_flash_memory=true,core_version=true,smart_enable=true,operation_mode=true,
 lan_ac_black_ips=true,lan_ac_black_macs=true,lan_ac_white_ips=true,lan_ac_white_macs=true}
 for i=#(m.children or {}),1,-1 do if m.children[i].sectiontype=="lan_ac_traffic" then table.remove(m.children,i) end end
 for _,section in ipairs(m.children or {}) do
  for i=#(section.children or {}),1,-1 do
   local field=section.children[i]
   if owned[field.option] then table.remove(section.children,i)
   elseif field.option=="Commit" or field.option=="Apply" then
    local apply=field.option=="Apply"
    function field.write()
     m.uci:commit("openclash")
     if apply then require("luci.sys").call("umask 077; (sleep 2; /usr/libexec/egressveil/setup apply >/var/run/egressveil/setup.log 2>&1) </dev/null &") end
    end
   end
  end
 end
 m.description="订阅、规则与兼容的高级选项保留 OpenClash 原有操作。接管方式、代理策略及 UA / TTL 请在配置引导中设置；当前使用 fake-IP 增强模式。"
 function m.on_after_commit() end

 return m
end
return M
