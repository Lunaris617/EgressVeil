include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-egressveil
PKG_VERSION:=0.1.0
PKG_RELEASE:=1
PKG_LICENSE:=GPL-3.0-only
PKG_LICENSE_FILES:=LICENSE
include $(INCLUDE_DIR)/package.mk

define Package/luci-app-egressveil
 SECTION:=luci
 CATEGORY:=LuCI
 SUBMENU:=3. Applications
 TITLE:=EgressVeil - unified WAN egress policy
 DEPENDS:=@TARGET_x86_64 +luci +luci-compat +ruby +ruby-yaml +ruby-digest +ruby-json +bash +unzip +ip-full +jsonfilter +nftables-json +firewall4 +dnsmasq-full +iptables-nft +iptables-mod-tproxy +iptables-mod-extra +iptables-mod-ipopt +iptables-mod-conntrack-extra +ipset +kmod-br-netfilter +kmod-ipt-nat +kmod-ipt-tproxy +kmod-nf-tproxy +kmod-nft-netdev +kmod-nft-compat +kmod-nft-fib +kmod-nft-tproxy +kmod-nf-conntrack-netlink +kmod-veth +kmod-tun +curl +ca-bundle
 CONFLICTS:=ua3f luci-app-openclash mihomo-ua3fi luci-app-ua3f-suite
endef

define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	$(INSTALL_DATA) ./LICENSE $(PKG_BUILD_DIR)/LICENSE
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
endef

define Package/luci-app-egressveil/install
	$(CP) ./files/* $(1)/
	find $(1)/etc/init.d $(1)/usr/libexec $(1)/etc/uci-defaults $(1)/etc/hotplug.d -type f -exec chmod 0755 {} +
	chmod -R 0755 $(1)/usr/share/openclash
	chmod 0755 $(1)/usr/bin/ua3f $(1)/etc/openclash/core/clash_meta
	chmod 0600 $(1)/etc/config/egressveil $(1)/etc/config/openclash
	$(LN) /etc/openclash/core/clash_meta $(1)/etc/openclash/clash
endef

$(eval $(call BuildPackage,luci-app-egressveil))
