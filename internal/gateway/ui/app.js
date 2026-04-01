const state = {
  ws: null,
  wsReady: false,
  sessions: [],
  agents: [],
  selectedSessionId: null,
  currentSession: null,
  pending: new Map(),
};

const els = {
  sessionList: document.getElementById('session-list'),
  sessionFilter: document.getElementById('session-filter'),
  sessionTitle: document.getElementById('session-title'),
  sessionMeta: document.getElementById('session-meta'),
  timeline: document.getElementById('timeline'),
  liveThinking: document.getElementById('live-thinking'),
  liveTools: document.getElementById('live-tools'),
  liveOutput: document.getElementById('live-output'),
  liveErrors: document.getElementById('live-errors'),
  wsStatus: document.getElementById('ws-status'),
  agentSelect: document.getElementById('agent-select'),
  promptInput: document.getElementById('prompt-input'),
  sendPrompt: document.getElementById('send-prompt'),
  newSession: document.getElementById('new-session'),
};

async function fetchJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Request failed: ${res.status}`);
  return res.json();
}

function formatTime(value) {
  if (!value) return 'n/a';
  return new Date(value).toLocaleString();
}

function escapeHTML(value = '') {
  return value.replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]));
}

function setWSStatus(connected) {
  state.wsReady = connected;
  els.wsStatus.textContent = connected ? 'Live' : 'Offline';
  els.wsStatus.className = connected ? 'status-pill' : 'status-pill offline';
}

function statusBadge(session) {
  const error = session.status === 'error' || session.last_error;
  const label = session.active ? 'live' : (session.status || 'idle');
  return `<span class="badge ${error ? 'error' : ''}">${escapeHTML(label)}</span>`;
}

function renderSessions() {
  const filter = els.sessionFilter.value.trim().toLowerCase();
  const items = state.sessions.filter((session) => {
    if (!filter) return true;
    return [session.id, session.agent_name, session.last_prompt, session.last_output, session.last_error].join(' ').toLowerCase().includes(filter);
  });
  if (!items.length) {
    els.sessionList.innerHTML = '<p class="empty">No sessions yet.</p>';
    return;
  }
  els.sessionList.innerHTML = items.map((session) => `
    <button class="session-item ${session.id === state.selectedSessionId ? 'active' : ''}" data-session-id="${escapeHTML(session.id)}">
      <div class="topline">
        <strong>${escapeHTML(session.id.slice(0, 8))}</strong>
        ${statusBadge(session)}
      </div>
      <p>${escapeHTML(session.agent_name || 'claude')}</p>
      <p class="muted">${escapeHTML(session.last_prompt || session.last_output || session.last_error || 'No activity yet')}</p>
      <p class="muted">${formatTime(session.last_active)}</p>
    </button>
  `).join('');
}

function appendTool(text) {
  const div = document.createElement('div');
  div.className = 'tool-entry';
  div.textContent = text;
  els.liveTools.prepend(div);
}

function appendError(text) {
  const div = document.createElement('div');
  div.className = 'error-entry';
  div.textContent = text;
  els.liveErrors.prepend(div);
}

function renderTimeline() {
  const activity = state.currentSession?.activity || [];
  if (!activity.length) {
    els.timeline.innerHTML = '<p class="empty">Choose a session to inspect prompts and stream history.</p>';
    return;
  }
  els.timeline.innerHTML = activity.slice().reverse().map((entry) => {
    let body = '';
    if (entry.type === 'prompt') body = entry.prompt;
    if (entry.type === 'result') body = entry.content;
    if (entry.type === 'error') body = entry.error;
    if (entry.type === 'event') {
      const event = entry.event || {};
      body = [event.type, event.name, event.content, event.input, event.output].filter(Boolean).join('\n');
    }
    return `
      <article class="timeline-entry ${escapeHTML(entry.type)}">
        <div class="topline">
          <strong>${escapeHTML(entry.type)}</strong>
          <span class="muted">${formatTime(entry.timestamp)}</span>
        </div>
        <pre>${escapeHTML(body)}</pre>
      </article>
    `;
  }).join('');
}

function resetLivePanels() {
  els.liveThinking.textContent = '';
  els.liveOutput.textContent = '';
  els.liveTools.innerHTML = '';
  els.liveErrors.innerHTML = '';
}

function renderSessionDetail() {
  const session = state.currentSession;
  if (!session) {
    els.sessionTitle.textContent = 'Select a session';
    els.sessionMeta.textContent = 'Active and historical session output appears here.';
    if (els.agentSelect.options.length > 0) els.agentSelect.selectedIndex = 0;
    renderTimeline();
    resetLivePanels();
    return;
  }
  els.sessionTitle.textContent = session.id;
  els.sessionMeta.textContent = `${session.agent_name || 'claude'} · ${session.status || 'idle'} · last active ${formatTime(session.last_active)}`;
  if ([...els.agentSelect.options].some((opt) => opt.value === session.agent_name)) {
    els.agentSelect.value = session.agent_name;
  }
  renderTimeline();
  rebuildLivePanels(session.activity || []);
}

function rebuildLivePanels(activity) {
  resetLivePanels();
  for (const entry of activity) {
    if (entry.type === 'prompt') continue;
    if (entry.type === 'event' && entry.event) {
      applyEventToLive(entry.event);
      continue;
    }
    if (entry.type === 'result') {
      els.liveOutput.textContent = entry.content || '';
      continue;
    }
    if (entry.type === 'error') {
      appendError(entry.error || 'Unknown error');
    }
  }
}

function applyEventToLive(event) {
  switch (event.type) {
    case 'thinking_delta':
    case 'thinking_end':
      els.liveThinking.textContent = [els.liveThinking.textContent, event.content || ''].filter(Boolean).join('\n');
      break;
    case 'tool_start':
      appendTool(`start: ${event.name || 'tool'}`);
      break;
    case 'tool_input':
      appendTool(`input ${event.name || 'tool'}\n${event.input || ''}`);
      break;
    case 'tool_output':
      appendTool(`output ${event.name || 'tool'}\n${event.output || ''}`);
      break;
    case 'tool_end':
      appendTool(`end: ${event.name || 'tool'}`);
      break;
    case 'output_delta':
    case 'output_final':
      els.liveOutput.textContent = [els.liveOutput.textContent, event.content || ''].filter(Boolean).join('');
      break;
    case 'error':
      appendError(event.content || 'Unknown error');
      if ((event.content || '').toLowerCase().includes('permission')) {
        appendError('Permission request failed or was denied. Retry with a different approval mode if needed.');
      }
      break;
  }
}

function upsertSession(summary) {
  const idx = state.sessions.findIndex((item) => item.id === summary.id);
  if (idx === -1) state.sessions.push(summary);
  else state.sessions[idx] = summary;
  state.sessions.sort((a, b) => new Date(b.last_active) - new Date(a.last_active));
}

async function loadSession(sessionId) {
  state.selectedSessionId = sessionId;
  const session = await fetchJSON(`/api/sessions/${encodeURIComponent(sessionId)}`);
  state.currentSession = session;
  history.replaceState({}, '', `/sessions/${encodeURIComponent(sessionId)}`);
  renderSessions();
  renderSessionDetail();
}

async function bootstrap() {
  const [sessionsRes, agentsRes] = await Promise.all([fetchJSON('/api/sessions'), fetchJSON('/api/agents')]);
  state.sessions = sessionsRes.sessions || [];
  state.agents = agentsRes.agents || [];
  if (!state.agents.length) state.agents = ['claude', 'codex'];
  els.agentSelect.innerHTML = state.agents.map((agent) => `<option value="${escapeHTML(agent)}">${escapeHTML(agent)}</option>`).join('');
  renderSessions();

  const route = location.pathname.match(/^\/sessions\/(.+)$/);
  const initialSession = route ? decodeURIComponent(route[1]) : state.sessions[0]?.id;
  if (initialSession) {
    await loadSession(initialSession);
  } else {
    renderSessionDetail();
  }
}

function rejectPendingRequests(message) {
  for (const [id, pending] of state.pending.entries()) {
    pending.reject(new Error(message));
    state.pending.delete(id);
  }
}

function rpc(method, params = {}) {
  return new Promise((resolve, reject) => {
    if (!state.ws || !state.wsReady || state.ws.readyState !== WebSocket.OPEN) {
      reject(new Error('WebSocket is not connected'));
      return;
    }
    const id = crypto.randomUUID();
    state.pending.set(id, { resolve, reject, method });
    state.ws.send(JSON.stringify({ jsonrpc: '2.0', id, method, params }));
  });
}

function connectWS() {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  state.ws = new WebSocket(`${protocol}//${location.host}/ws`);
  setWSStatus(false);

  state.ws.addEventListener('open', () => {
    setWSStatus(true);
  });

  state.ws.addEventListener('close', () => {
    setWSStatus(false);
    rejectPendingRequests('WebSocket disconnected');
    setTimeout(connectWS, 1000);
  });

  state.ws.addEventListener('message', async (raw) => {
    const message = JSON.parse(raw.data);
    if (message.id && state.pending.has(message.id) && !message.method) {
      const pending = state.pending.get(message.id);
      state.pending.delete(message.id);
      if (message.error) pending.reject(new Error(message.error.message));
      else pending.resolve(message.result);
      return;
    }

    if (message.method === 'session.snapshot') {
      for (const summary of message.params.sessions || []) upsertSession(summary);
      renderSessions();
      return;
    }

    if (message.method === 'session.updated') {
      upsertSession(message.params.session);
      renderSessions();
      if (message.params.session.id === state.selectedSessionId && state.currentSession) {
        state.currentSession = { ...state.currentSession, ...message.params.session };
        renderSessionDetail();
      }
      return;
    }

    if (message.method === 'session.deleted') {
      state.sessions = state.sessions.filter((session) => session.id !== message.params.session_id);
      if (state.selectedSessionId === message.params.session_id) {
        state.selectedSessionId = state.sessions[0]?.id || null;
        state.currentSession = null;
        if (state.selectedSessionId) await loadSession(state.selectedSessionId);
        else renderSessionDetail();
      }
      renderSessions();
      return;
    }

    if (message.method === 'session.activity') {
      const { session_id: sessionId, activity } = message.params;
      if (sessionId === state.selectedSessionId && state.currentSession) {
        state.currentSession.activity = [...(state.currentSession.activity || []), activity];
        if (activity.type === 'prompt') {
          resetLivePanels();
        }
        renderTimeline();
        if (activity.type === 'event' && activity.event) applyEventToLive(activity.event);
        if (activity.type === 'result') els.liveOutput.textContent = activity.content || '';
        if (activity.type === 'error') appendError(activity.error || 'Unknown error');
      }
      return;
    }
  });
}

els.sessionList.addEventListener('click', async (event) => {
  const button = event.target.closest('[data-session-id]');
  if (!button) return;
  await loadSession(button.dataset.sessionId);
});

els.sessionFilter.addEventListener('input', renderSessions);

els.newSession.addEventListener('click', async () => {
  const agent = els.agentSelect.value || state.currentSession?.agent_name || state.agents[0];
  const created = await rpc('session.new', { agent });
  upsertSession(created);
  await loadSession(created.id);
});

els.agentSelect.addEventListener('change', async () => {
  if (!state.selectedSessionId) return;
  const updated = await rpc('session.update', { session_id: state.selectedSessionId, agent: els.agentSelect.value });
  upsertSession(updated);
  await loadSession(updated.id);
});

async function sendPrompt() {
  if (!state.selectedSessionId) return;
  const content = els.promptInput.value.trim();
  if (!content) return;
  resetLivePanels();
  try {
    await rpc('ask_stream', { session_id: state.selectedSessionId, agent: els.agentSelect.value, content });
    els.promptInput.value = '';
    await loadSession(state.selectedSessionId);
  } catch (error) {
    appendError(error.message);
  }
}

els.sendPrompt.addEventListener('click', sendPrompt);
els.promptInput.addEventListener('keydown', (event) => {
  if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
    event.preventDefault();
    void sendPrompt();
  }
});

connectWS();
bootstrap().catch((error) => {
  els.sessionMeta.textContent = error.message;
});
