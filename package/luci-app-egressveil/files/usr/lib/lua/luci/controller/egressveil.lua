-- SPDX-License-Identifier: GPL-3.0-only
module("luci.controller.egressveil", package.seeall)
function index()
 local p=entry({"admin","services","egressveil"},cbi("egressveil"),"EgressVeil",29)
 p.dependent=false
end
