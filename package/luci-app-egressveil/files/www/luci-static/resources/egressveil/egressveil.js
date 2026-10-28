/* SPDX-License-Identifier: GPL-3.0-only */
(() => {
  'use strict';
  function start() {
    const map = document.getElementById('cbi-egressveil');
    const node = document.getElementById('cbi-egressveil-main');
    if (!map || !node) return false;
    if (map.dataset.evReady) return true;
    const row = key => document.getElementById('cbi-egressveil-main-' + key);
    const keys = ['proxy_enabled', 'ua_enabled', 'ttl_enabled'];
    if (!keys.every(key => row(key)?.querySelector('select'))) return false;
    map.dataset.evReady = '1';
    map.classList.add('ev-app');
    const el = (tag, cls, text) => {
      const item = document.createElement(tag);
      item.className = cls;
      if (text) item.textContent = text;
      return item;
    };
    const hero = el('div', 'ev-hero');
    const intro = el('div', 'ev-intro');
    intro.append(el('div', 'ev-eyebrow', 'EGRESSVEIL'), el('h2', '', '反-防代理检测配置'),
      el('p', 'ev-motto', '被命令的自由，也算是一种不自由吧…'));
    hero.append(intro, el('span', 'ev-tag', 'IPv4 · 增强模式'));
    node.prepend(hero);
    const grid = el('div', 'ev-grid');
    node.append(grid);
    const cards = {};
    function card(kind, icon, title, description, fields) {
      const item = el('article', 'ev-card ev-' + kind);
      const header = el('div', 'ev-card-header');
      header.append(el('span', 'ev-icon', icon));
      const text = el('div', 'ev-card-heading');
      text.append(el('h3', '', title), el('p', '', description));
      header.append(text); item.append(header);
      fields.forEach(key => { if (row(key)) item.append(row(key)); });
      grid.append(item); cards[kind] = item;
      return item;
    }
    card('general', '↗', '运行控制', '暂停服务会保留你的功能选择。', ['enabled']);
    card('proxy', 'P', '代理与订阅', '可暂不配置，后续随时启用。',
      ['proxy_enabled', 'feed', '_refresh_feed', 'proxy_config', 'proxy_mode', 'interval']);
    card('ua', 'UA', 'HTTP User-Agent', '统一明文请求标识，HTTPS 保持加密。', ['ua_enabled', 'ua', 'log_level']);
    card('ttl', 'TTL', 'IPv4 TTL', '按设定值统一 WAN 出站 TTL。', ['ttl_enabled', 'ttl']);
    card('network', '⇄', '接口设置', '使用 OpenWrt 的逻辑接口名称。', ['lan', 'wan']);
    card('runtime', '●', '运行状态', '保存应用后刷新，可查看当前就绪状态。', ['_state', '_health', '_runtime', '_ttl_runtime']);
    const switchFor = (input, title, read, write) => {
      const wrap = el('div', 'ev-toggle-wrap');
      const label = el('span', 'ev-toggle-state');
      const button = el('button', 'ev-switch');
      button.type = 'button'; button.setAttribute('role', 'switch');
      button.setAttribute('aria-label', title);
      button.append(el('span', 'ev-switch-thumb'));
      const choices = el('div', 'ev-choice-group');
      ['关闭', '开启'].forEach((text, i) => {
        const choice = el('button', 'ev-choice', text); choice.type = 'button';
        choice.addEventListener('click', () => write(String(i))); choices.append(choice);
      });
      const paint = () => {
        const value = read(); const chosen = value === '0' || value === '1';
        wrap.dataset.state = chosen ? value : 'unset';
        label.textContent = chosen ? (value === '1' ? '已开启' : '已关闭') : '请选择开启或关闭';
        button.hidden = !chosen; choices.hidden = chosen;
        button.setAttribute('aria-checked', value === '1' ? 'true' : 'false');
      };
      button.addEventListener('click', () => write(read() === '1' ? '0' : '1'));
      input.addEventListener('change', paint);
      wrap.append(label, choices, button); paint(); return wrap;
    };
    keys.forEach(key => {
      const field = row(key); const select = field.querySelector('select');
      select.classList.add('ev-source-input'); select.tabIndex = -1;
      select.setAttribute('aria-hidden', 'true');
      field.classList.add('ev-feature-toggle');
      const write = value => { select.value = value; select.dispatchEvent(new Event('change', {bubbles: true})); };
      select.parentElement.append(switchFor(select, field.querySelector('label')?.textContent || key, () => select.value, write));
    });
    const master = row('enabled'); const checkbox = master.querySelector('input[type="checkbox"]');
    if (checkbox) {
      master.classList.add('ev-feature-toggle');
      const original = checkbox.closest('.cbi-checkbox') || checkbox;
      original.classList.add('ev-source-input');
      checkbox.tabIndex = -1;
      original.parentElement.append(switchFor(checkbox, '运行 EgressVeil', () => checkbox.checked ? '1' : '0', () => checkbox.click()));
    }
    const heading = node.parentElement.querySelector(':scope > legend, :scope > h3');
    if (heading) heading.hidden = true;
  }
  if (!start()) {
    const observer = new MutationObserver(() => { if (start()) observer.disconnect(); });
    observer.observe(document.documentElement, {childList: true, subtree: true});
    setTimeout(() => observer.disconnect(), 15000);
  }
})();
