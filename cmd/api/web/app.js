// 前端逻辑：登录 -> 账号管理 -> 手动签到 -> 日志展示。
let accounts = [];

async function login() {
  const username = document.getElementById('username').value.trim();
  const password = document.getElementById('password').value;
  const r = await fetch('/api/login', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({username, password}),
  });
  if (!r.ok) { alert('登录失败：' + await r.text()); return; }
  document.getElementById('loginCard').classList.add('hidden');
  document.getElementById('mainCard').classList.remove('hidden');
  loadAccounts();
  loadLogs();
}

async function loadAccounts() {
  const r = await fetch('/api/accounts');
  if (!r.ok) return;
  const data = await r.json();
  // 本地维护密码：新增时填，展示时不回显
  const known = Object.fromEntries(accounts.map(a => [a.username, a.password || '']));
  accounts = (data.accounts || []).map(a => ({username: a.username, password: known[a.username] || ''}));
  renderAccounts();
}

function renderAccounts() {
  const el = document.getElementById('accounts');
  el.innerHTML = accounts.map((a, i) =>
    `<div>${a.username} ${a.password ? '' : '(密码未在本地，需重新输入后保存)'} ` +
    `<button onclick="delAccount(${i})">删除</button></div>`).join('') || '暂无账号';
}

function addAccount() {
  const u = document.getElementById('newUser').value.trim();
  const p = document.getElementById('newPass').value;
  if (!u || !p) { alert('请输入手机号和密码'); return; }
  accounts.push({username: u, password: p});
  document.getElementById('newUser').value = '';
  document.getElementById('newPass').value = '';
  renderAccounts();
}

async function saveAccounts() {
  const r = await fetch('/api/accounts', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({accounts}),
  });
  alert(r.ok ? '保存成功' : '保存失败：' + await r.text());
  loadAccounts();
}

async function delAccount(i) {
  const username = accounts[i].username;
  const r = await fetch('/api/accounts?username=' + encodeURIComponent(username), {method: 'DELETE'});
  if (r.ok) { accounts.splice(i, 1); renderAccounts(); }
}

async function runCheckin() {
  document.getElementById('checkinResult').textContent = '签到中...';
  const r = await fetch('/api/checkin', {method: 'POST'});
  const data = await r.json();
  document.getElementById('checkinResult').textContent = JSON.stringify(data, null, 2);
  loadLogs();
}

async function loadLogs() {
  const r = await fetch('/api/logs?limit=50');
  if (!r.ok) return;
  const data = await r.json();
  document.getElementById('logs').innerHTML = (data.logs || []).map(l =>
    `<tr><td>${l.time || ''}</td><td>${l.user || ''}</td>` +
    `<td>${JSON.stringify(l.personal || {})}</td>` +
    `<td>${JSON.stringify(l.families || [])}</td>` +
    `<td>${l.error || ''}</td></tr>`).join('');
}
