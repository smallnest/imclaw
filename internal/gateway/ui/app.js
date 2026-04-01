const state = {
  ws: null,
  wsReady: false,
  sessions: [],
  agents: [],
  selectedSessionId: null,
  currentSession: null,
  pending: new Map(),
  isStreaming: false,
};

const els = {
  sessionList: document.getElementById('session-list'),
  sessionFilter: document.getElementById('session-filter'),
  sessionTitle: document.getElementById('session-title'),
  sessionMeta: document.getElementById('session-meta'),
  messages: document.getElementById('messages'),
  wsStatus: document.getElementById('ws-status'),
  agentSelect: document.getElementById('agent-select'),
  promptInput: document.getElementById('prompt-input'),
  sendPrompt: document.getElementById('send-prompt'),
  newSession: document.getElementById('new-session'),
};

// ============ Utility Functions ============

async function fetchJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Request failed: ${res.status}`);
  return res.json();
}

function formatTime(value) {
  if (!value) return 'n/a';
  return new Date(value).toLocaleString();
}

function formatRelativeTime(value) {
  if (!value) return '';
  const date = new Date(value);
  const now = new Date();
  const diff = now - date;
  const minutes = Math.floor(diff / 60000);
  const hours = Math.floor(diff / 3600000);
  const days = Math.floor(diff / 86400000);

  if (minutes < 1) return '刚刚';
  if (minutes < 60) return `${minutes}分钟前`;
  if (hours < 24) return `${hours}小时前`;
  if (days < 7) return `${days}天前`;
  return date.toLocaleDateString('zh-CN');
}

function escapeHTML(value = '') {
  return value.replace(/[&<>"']/g, (char) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  }[char]));
}

// Render markdown using marked library
function markdownToHTML(text) {
  if (!text) return '';
  return marked.parse(text, {
    breaks: true,
    gfm: true
  });
}

// Filter out status messages and ANSI remnants
function filterStatusMessages(content) {
  if (!content) return content;

  // Remove common ANSI escape code remnants
  content = content.replace(/\x1b\[[0-9;]*[a-zA-Z]/g, '');
  content = content.replace(/\[[0-9]+m/g, '');

  return content
    .split('\n')
    .filter(line => {
      const trimmed = line.trim();

      // Skip empty lines
      if (!trimmed) return false;

      // Skip lines that start with status markers
      if (trimmed.startsWith('[acpx]') ||
          trimmed.startsWith('[client]') ||
          trimmed.startsWith('[done]') ||
          trimmed.startsWith('[thinking]') ||
          trimmed.startsWith('[tool]')) {
        return false;
      }

      // Skip lines that are only ANSI remnants like [2m, [0m, etc.
      if (/^\[[0-9;]+[a-zA-Z]\]?$/.test(trimmed)) {
        return false;
      }

      return true;
    })
    .join('\n')
    .trim();
}

function setWSStatus(connected) {
  state.wsReady = connected;
  els.wsStatus.textContent = connected ? '在线' : '离线';
  els.wsStatus.className = `status-indicator ${connected ? 'live' : 'offline'}`;
}

// ============ Session List ============

function getSessionTitle(session) {
  // Use first prompt as title, max 20 characters
  const title = session.first_prompt || session.last_prompt;
  if (title) {
    return title.slice(0, 20) + (title.length > 20 ? '...' : '');
  }
  return `对话 ${session.id.slice(0, 8)}`;
}

function getSessionPreview(session) {
  if (session.last_error) return session.last_error.slice(0, 30);
  if (session.last_output) return session.last_output.slice(0, 30);
  return '暂无消息';
}

function renderSessions() {
  const filter = els.sessionFilter.value.trim().toLowerCase();
  const items = state.sessions.filter((session) => {
    if (!filter) return true;
    return [session.id, session.agent_name, session.last_prompt, session.last_output, session.last_error]
      .join(' ').toLowerCase().includes(filter);
  });

  if (!items.length) {
    els.sessionList.innerHTML = '<div class="empty-state">暂无对话</div>';
    return;
  }

  els.sessionList.innerHTML = items.map((session) => `
    <button class="session-item ${session.id === state.selectedSessionId ? 'active' : ''}"
            data-session-id="${escapeHTML(session.id)}">
      <div class="session-title">${escapeHTML(getSessionTitle(session))}</div>
      <div class="session-preview">${escapeHTML(getSessionPreview(session))}</div>
      <div class="session-time">${formatRelativeTime(session.last_active)}</div>
    </button>
  `).join('');
}

function upsertSession(summary) {
  const idx = state.sessions.findIndex((item) => item.id === summary.id);
  if (idx === -1) state.sessions.push(summary);
  else state.sessions[idx] = summary;
  state.sessions.sort((a, b) => new Date(b.last_active) - new Date(a.last_active));
  renderSessions();
}

// ============ Message Rendering ============

function createMessageElement(role, content = '') {
  const div = document.createElement('div');
  div.className = `message message-${role}`;

  if (role === 'user') {
    div.innerHTML = `
      <div class="message-content">
        <div class="bubble">${escapeHTML(content)}</div>
      </div>
    `;
  } else {
    div.innerHTML = `
      <div class="message-content">
        <div class="message-header">
          <div class="avatar">${state.currentSession?.agent_name?.[0]?.toUpperCase() || 'A'}</div>
          <span class="role-name">${state.currentSession?.agent_name || 'Assistant'}</span>
        </div>
        <div class="bubble">
          <div class="output-content"></div>
        </div>
      </div>
    `;
  }

  return div;
}

function createThinkingBlock(content) {
  const filteredContent = filterStatusMessages(content);
  if (!filteredContent || !filteredContent.trim()) return null;

  const div = document.createElement('div');
  div.className = 'thinking-block collapsible';
  div.innerHTML = `
    <div class="collapsible-header" onclick="toggleCollapse(this)">
      <span class="label">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M9 18h6"/>
          <path d="M10 22h4"/>
          <path d="M12 2a7 7 0 0 0-4 12.7V17h8v-2.3A7 7 0 0 0 12 2z"/>
        </svg>
        思考
      </span>
      <span class="toggle-icon">▼</span>
    </div>
    <div class="collapsible-content">
      <pre>${escapeHTML(filteredContent)}</pre>
    </div>
  `;
  return div;
}

function createToolBlock(name, details) {
  if (!name || name === 'Terminal') return null;
  const div = document.createElement('div');
  div.className = 'tool-block collapsible';
  const bodyText = details || '调用完成';
  div.innerHTML = `
    <div class="collapsible-header" onclick="toggleCollapse(this)">
      <svg class="tool-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>
      </svg>
      <span class="tool-name">工具</span>
      <span class="toggle-icon">▼</span>
    </div>
    <div class="collapsible-content">
      <div class="tool-title">${escapeHTML(name)}</div>
      <pre>${escapeHTML(bodyText)}</pre>
    </div>
  `;
  return div;
}

// Global function for toggling collapse
window.toggleCollapse = function(header) {
  const block = header.parentElement;
  const content = block.querySelector('.collapsible-content');
  const icon = header.querySelector('.toggle-icon');

  if (content.style.display === 'none') {
    content.style.display = 'block';
    icon.textContent = '▼';
    block.classList.remove('collapsed');
  } else {
    content.style.display = 'none';
    icon.textContent = '▶';
    block.classList.add('collapsed');
  }
};

function createErrorBlock(content) {
  const div = document.createElement('div');
  div.className = 'error-block';
  div.innerHTML = `
    <div class="label">错误</div>
    <pre>${escapeHTML(content)}</pre>
  `;
  return div;
}

function addThinkingBlock(record, content) {
  const filteredContent = filterStatusMessages(content);
  if (!filteredContent) return;
  record.blocks.push({ kind: 'thinking', content: filteredContent });
}

function addToolBlock(record, event) {
  if (!event || !event.name || event.name === 'Terminal') return;
  const lines = [];
  if (event.input) lines.push(`输入：${event.input}`);
  if (event.output) lines.push(`输出：${event.output}`);
  if (event.content) lines.push(event.content);
  const details = lines.length ? lines.join('\n\n') : '调用完成';
  record.blocks.push({ kind: 'tool', name: event.name, details });
}

function addErrorBlock(record, content) {
  const filtered = filterStatusMessages(content || '未知错误');
  if (!filtered) return;
  record.blocks.push({ kind: 'error', content: filtered });
}

function setRecordOutput(record, content, isFinal) {
  if (!content) return;
  record.finalContent = filterStatusMessages(content);
  record.isFinal = isFinal;
}

function scrollToBottom() {
  // Use requestAnimationFrame to ensure scroll happens after DOM updates
  requestAnimationFrame(() => {
    els.messages.scrollTop = els.messages.scrollHeight;
  });
}

function renderMessages() {
  const activity = state.currentSession?.activity || [];

  if (!activity.length) {
    els.messages.innerHTML = `
      <div class="welcome-screen">
        <div class="welcome-icon">💬</div>
        <h2>开始对话</h2>
        <p>发送消息开始对话</p>
      </div>
    `;
    return;
  }

  const records = new Map();
  const renderQueue = [];

  function getOrCreateRecord(requestId) {
    const key = requestId || '__orphan__';
    if (!records.has(key)) {
      records.set(key, { element: createMessageElement('assistant'), blocks: [], finalContent: '', slotQueued: false });
    }
    return records.get(key);
  }

  function queueAssistant(requestId) {
    const key = requestId || '__orphan__';
    const record = getOrCreateRecord(requestId);
    if (!record.slotQueued) {
      renderQueue.push({ type: 'assistant', requestId: key });
      record.slotQueued = true;
    }
    return record;
  }

  for (const entry of activity) {
    if (entry.type === 'prompt') {
      const userNode = createMessageElement('user', entry.prompt);
      renderQueue.push({ type: 'user', node: userNode });
      queueAssistant(entry.request_id);
      continue;
    }

    const record = getOrCreateRecord(entry.request_id);
    if (entry.type === 'result') {
      setRecordOutput(record, entry.content, true);
      continue;
    }
    if (entry.type === 'error') {
      addErrorBlock(record, entry.error);
      continue;
    }
    if (entry.type !== 'event' || !entry.event) continue;

    const event = entry.event;
    switch (event.type) {
      case 'thinking_end':
        addThinkingBlock(record, event.content);
        break;
      case 'tool_end':
        addToolBlock(record, event);
        break;
      case 'output_delta':
        setRecordOutput(record, event.content, false);
        break;
      case 'output_final':
        setRecordOutput(record, event.content, true);
        break;
      case 'error':
        addErrorBlock(record, event.content);
        break;
    }
  }

  for (const [key, record] of records) {
    if (!record.slotQueued) {
      renderQueue.push({ type: 'assistant', requestId: key });
      record.slotQueued = true;
    }
  }

  els.messages.innerHTML = '';
  for (const item of renderQueue) {
    if (item.type === 'user') {
      els.messages.appendChild(item.node);
      continue;
    }
    const record = records.get(item.requestId);
    if (!record) continue;
    const bubble = record.element.querySelector('.bubble');
    bubble.innerHTML = '';
    for (const block of record.blocks) {
      let blockEl;
      if (block.kind === 'thinking') {
        blockEl = createThinkingBlock(block.content);
      } else if (block.kind === 'tool') {
        blockEl = createToolBlock(block.name, block.details);
      } else if (block.kind === 'error') {
        blockEl = createErrorBlock(block.content);
      }
      if (blockEl) bubble.appendChild(blockEl);
    }
    const outputEl = document.createElement('div');
    outputEl.className = 'output-content';
    // Only render markdown when output is final, otherwise show plain text
    if (record.isFinal) {
      outputEl.innerHTML = markdownToHTML(record.finalContent || '');
    } else {
      outputEl.innerHTML = escapeHTML(record.finalContent || '').replace(/\n/g, '<br>');
    }
    bubble.appendChild(outputEl);
    els.messages.appendChild(record.element);
  }

  scrollToBottom();
}

// ============ Session Loading ============

async function loadSession(sessionId) {
  state.selectedSessionId = sessionId;

  try {
    const session = await fetchJSON(`/api/sessions/${encodeURIComponent(sessionId)}`);
    state.currentSession = session;
    history.replaceState({}, '', `/sessions/${encodeURIComponent(sessionId)}`);

    // Update header
    els.sessionTitle.textContent = getSessionTitle(session) || '新对话';
    els.sessionMeta.textContent = `${session.agent_name || 'claude'} · ${formatRelativeTime(session.last_active)}`;

    // Update agent select
    if ([...els.agentSelect.options].some((opt) => opt.value === session.agent_name)) {
      els.agentSelect.value = session.agent_name;
    }

    renderSessions();
    renderMessages();
  } catch (error) {
    console.error('Failed to load session:', error);
  }
}

// ============ WebSocket ============

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
    console.log('WebSocket connected');
  });

  state.ws.addEventListener('close', () => {
    setWSStatus(false);
    console.log('WebSocket disconnected');
    // Reject pending requests
    for (const [id, pending] of state.pending.entries()) {
      pending.reject(new Error('WebSocket disconnected'));
    }
    state.pending.clear();
    // Reconnect
    setTimeout(connectWS, 1000);
  });

  state.ws.addEventListener('message', async (raw) => {
    const message = JSON.parse(raw.data);

    // RPC response
    if (message.id && state.pending.has(message.id) && !message.method) {
      const pending = state.pending.get(message.id);
      state.pending.delete(message.id);
      if (message.error) pending.reject(new Error(message.error.message));
      else pending.resolve(message.result);
      return;
    }

    // Notifications
    if (message.method === 'session.snapshot') {
      for (const summary of message.params.sessions || []) upsertSession(summary);
      return;
    }

    if (message.method === 'session.updated') {
      upsertSession(message.params.session);
      if (message.params.session.id === state.selectedSessionId && state.currentSession) {
        state.currentSession = { ...state.currentSession, ...message.params.session };
        els.sessionTitle.textContent = getSessionTitle(state.currentSession);
      }
      return;
    }

    if (message.method === 'session.deleted') {
      state.sessions = state.sessions.filter((s) => s.id !== message.params.session_id);
      if (state.selectedSessionId === message.params.session_id) {
        state.selectedSessionId = state.sessions[0]?.id || null;
        state.currentSession = null;
        if (state.selectedSessionId) await loadSession(state.selectedSessionId);
        else renderMessages();
      }
      renderSessions();
      return;
    }

    if (message.method === 'session.activity') {
      const { session_id: sessionId, activity } = message.params;
      if (sessionId === state.selectedSessionId && state.currentSession) {
        state.currentSession.activity = [...(state.currentSession.activity || []), activity];
        renderMessages();
        // Check if streaming is complete
        if (activity.type === 'event' && activity.event) {
          const eventType = activity.event.type;
          if (eventType === 'output_final' || eventType === 'done' || eventType === 'error') {
            state.isStreaming = false;
            els.sendPrompt.disabled = false;
            els.promptInput.disabled = false;
            els.promptInput.focus();
          }
        } else if (activity.type === 'result' || activity.type === 'error') {
          state.isStreaming = false;
          els.sendPrompt.disabled = false;
          els.promptInput.disabled = false;
          els.promptInput.focus();
        }
      }
      return;
    }
  });
}

// ============ Bootstrap ============

async function bootstrap() {
  const [sessionsRes, agentsRes] = await Promise.all([
    fetchJSON('/api/sessions'),
    fetchJSON('/api/agents')
  ]);

  state.sessions = sessionsRes.sessions || [];
  state.agents = agentsRes.agents || [];
  if (!state.agents.length) state.agents = ['claude', 'codex'];

  els.agentSelect.innerHTML = state.agents
    .map((agent) => `<option value="${escapeHTML(agent)}">${escapeHTML(agent)}</option>`)
    .join('');

  renderSessions();

  // Load initial session
  const route = location.pathname.match(/^\/sessions\/(.+)$/);
  const initialSession = route ? decodeURIComponent(route[1]) : state.sessions[0]?.id;

  if (initialSession) {
    await loadSession(initialSession);
  } else {
    renderMessages();
  }
}

// ============ Event Handlers ============

els.sessionList.addEventListener('click', async (event) => {
  const button = event.target.closest('[data-session-id]');
  if (!button) return;
  await loadSession(button.dataset.sessionId);
});

els.sessionFilter.addEventListener('input', renderSessions);

els.newSession.addEventListener('click', async () => {
  try {
    const agent = els.agentSelect.value || state.agents[0];
    const created = await rpc('session.new', { agent });
    upsertSession(created);
    await loadSession(created.id);
  } catch (error) {
    console.error('Failed to create session:', error);
  }
});

els.agentSelect.addEventListener('change', async () => {
  if (!state.selectedSessionId) return;
  try {
    const updated = await rpc('session.update', {
      session_id: state.selectedSessionId,
      agent: els.agentSelect.value
    });
    upsertSession(updated);
    await loadSession(updated.id);
  } catch (error) {
    console.error('Failed to update agent:', error);
  }
});

// Auto-resize textarea
els.promptInput.addEventListener('input', () => {
  els.promptInput.style.height = 'auto';
  els.promptInput.style.height = Math.min(els.promptInput.scrollHeight, 200) + 'px';
});

// Send prompt
async function sendPrompt() {
  // Prevent sending while streaming
  if (state.isStreaming) return;

  if (!state.selectedSessionId) {
    // Create new session first
    try {
      const agent = els.agentSelect.value || state.agents[0];
      const created = await rpc('session.new', { agent });
      upsertSession(created);
      await loadSession(created.id);
    } catch (error) {
      console.error('Failed to create session:', error);
      return;
    }
  }

  const content = els.promptInput.value.trim();
  if (!content) return;

  if (!state.wsReady) {
    alert('WebSocket is not connected. Please wait...');
    return;
  }

  try {
    // Disable input while streaming
    state.isStreaming = true;
    els.sendPrompt.disabled = true;
    els.promptInput.disabled = true;

    // Clear input
    els.promptInput.value = '';
    els.promptInput.style.height = 'auto';

    // Send to backend
    await rpc('ask_stream', {
      session_id: state.selectedSessionId,
      agent: els.agentSelect.value,
      content,
      permissions: 'approve-all',
    });

    // Note: Input will be re-enabled when we receive output_final/done/error event
  } catch (error) {
    console.error('Failed to send prompt:', error);
    state.isStreaming = false;
    els.sendPrompt.disabled = false;
    els.promptInput.disabled = false;
    await loadSession(state.selectedSessionId);
  }
}

els.sendPrompt.addEventListener('click', sendPrompt);

els.promptInput.addEventListener('keydown', (event) => {
  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault();
    sendPrompt();
  }
});

// ============ Initialize ============

connectWS();
bootstrap().catch((error) => {
  console.error('Bootstrap failed:', error);
});
