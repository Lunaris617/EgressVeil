-- SPDX-License-Identifier: GPL-3.0-only
module("luci.controller.egressveil", package.seeall)
function index()
 entry({"admin","services","egressveil"},alias("admin","services","openclash","setup"),nil).dependent=false
end
