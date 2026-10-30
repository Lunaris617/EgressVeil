include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-egressveil
PKG_VERSION:=1.0.0
PKG_RELEASE:=1
PKG_BUILD_DEPENDS:=lua/host
PKG_MAINTAINER:=Lunaris617
PKG_LICENSE:=GPL-3.0-only
PKG_LICENSE_FILES:=LICENSE
include $(INCLUDE_DIR)/package.mk
# These prebuilt executables are frozen and verified at runtime. SDK rstrip
# must not change their bytes (including the ethtool helper trust anchor).
RSTRIP:=:

define Package/luci-app-egressveil
 SECTION:=luci
 CATEGORY:=LuCI
 SUBMENU:=3. Applications
 TITLE:=EgressVeil - UA3F, OpenClash and TTL
 URL:=https://github.com/Lunaris617/EgressVeil
 DEPENDS:=@TARGET_x86_64 +luci +luci-compat +ruby +ruby-yaml +ruby-digest +ruby-json +bash +unzip +ip-full +jsonfilter +nftables-json +firewall4 +dnsmasq-full +iptables-nft +iptables-mod-tproxy +iptables-mod-extra +iptables-mod-ipopt +iptables-mod-conntrack-extra +ipset +kmod-br-netfilter +kmod-ipt-nat +kmod-ipt-tproxy +kmod-nf-tproxy +kmod-nft-netdev +kmod-nft-compat +kmod-nft-fib +kmod-nft-tproxy +kmod-nf-conntrack-netlink +kmod-veth +kmod-tun +curl +ca-bundle +coreutils-timeout
 CONFLICTS:=ua3f luci-app-openclash mihomo-ua3fi luci-app-ua3f-suite
endef

define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	$(INSTALL_DATA) ./LICENSE $(PKG_BUILD_DIR)/LICENSE
	$(foreach po,$(wildcard $(CURDIR)/po/zh-cn/*.po),po2lmo $(po) $(PKG_BUILD_DIR)/$(patsubst %.po,%.lmo,$(notdir $(po)));)
endef

define Build/Configure
endef

define Build/Compile
endef

define Package/luci-app-egressveil/conffiles
/etc/config/egressveil
/etc/config/ua3f
/etc/config/openclash
/etc/ua3f-integration.conf
/etc/egressveil/
/etc/openclash/config/
/etc/openclash/proxy_provider/
/etc/openclash/rule_provider/
endef

define Package/luci-app-egressveil/install
	$(CP) ./files/* $(1)/
	find $(1) -type d -exec chmod 0755 {} +
	find $(1) -type f -exec chmod 0644 {} +
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/i18n
	$(INSTALL_DATA) $(PKG_BUILD_DIR)/*.lmo $(1)/usr/lib/lua/luci/i18n/
	find $(1)/etc/init.d $(1)/usr/libexec $(1)/etc/uci-defaults $(1)/etc/hotplug.d -type f -exec chmod 0755 {} +
	chmod -R 0755 $(1)/usr/share/openclash
	chmod 0755 $(1)/usr/bin/ua3f $(1)/etc/openclash/core/clash_meta
	chmod 0600 $(1)/etc/config/egressveil $(1)/etc/config/openclash
	$(LN) /etc/openclash/core/clash_meta $(1)/etc/openclash/clash
endef

$(eval $(call BuildPackage,luci-app-egressveil))
