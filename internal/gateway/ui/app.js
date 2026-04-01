const state = {
  ws: null,
  wsReady: false,
  sessions: [],
  agents: [],
  selectedSessionId: null,
  currentSession: null,
  pending: new Map(),
  currentMessage: null, // Current streaming message element
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

  if (minutes < 1) return 'Just now';
  if (minutes < 60) return `${minutes}m ago`;
  if (hours < 24) return `${hours}h ago`;
  if (days < 7) return `${days}d ago`;
  return date.toLocaleDateString();
}

function escapeHTML(value = '') {
  return value.replace(/[&<>"']/g, (char) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  }[char]));
}

// Filter out status messages like [acpx], [client], and [done]
function filterStatusMessages(content) {
  if (!content) return content;
  return content
    .split('\n')
    .filter(line => {
      const trimmed = line.trim();
      // Skip lines that start with [acpx], [client], or [done]
      if (trimmed.startsWith('[acpx]') || trimmed.startsWith('[client]') || trimmed.startsWith('[done]')) {
        return false;
      }
      return true;
    })
    .join('\n')
    .trim();
}

function setWSStatus(connected) {
  state.wsReady = connected;
  els.wsStatus.textContent = connected ? 'Live' : 'Offline';
  els.wsStatus.className = `status-indicator ${connected ? 'live' : 'offline'}`;
}

// ============ Session List ============

function getSessionTitle(session) {
  if (session.last_prompt) {
    return session.last_prompt.slice(0, 50) + (session.last_prompt.length > 50 ? '...' : '');
  }
  return `Chat ${session.id.slice(0, 8)}`;
}

function getSessionPreview(session) {
  if (session.last_error) return session.last_error.slice(0, 30);
  if (session.last_output) return session.last_output.slice(0, 30);
  return 'No messages yet';
}

function renderSessions() {
  const filter = els.sessionFilter.value.trim().toLowerCase();
  const items = state.sessions.filter((session) => {
    if (!filter) return true;
    return [session.id, session.agent_name, session.last_prompt, session.last_output, session.last_error]
      .join(' ').toLowerCase().includes(filter);
  });

  if (!items.length) {
    els.sessionList.innerHTML = '<div class="empty-state">No conversations</div>';
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
      <span class="label">💭 Thinking</span>
      <span class="toggle-icon">▼</span>
    </div>
    <div class="collapsible-content">
      <pre>${escapeHTML(filteredContent)}</pre>
    </div>
  `;
  return div;
}

function createToolBlock(name, type, content) {
  // Skip empty tool events (start/end with no content)
  if ((type === 'start' || type === 'end') && !content) {
    return null;
  }

  const div = document.createElement('div');
  div.className = 'tool-block collapsible';

  const icon = type === 'start' ? '▶' : type === 'end' ? '⏹' : '📄';
  const hasContent = content && content.trim();

  div.innerHTML = `
    <div class="collapsible-header ${hasContent ? '' : 'no-content'}" ${hasContent ? 'onclick="toggleCollapse(this)"' : ''}>
      <span class="label"><span class="tool-name">${icon} ${escapeHTML(name)}</span></span>
      ${hasContent ? '<span class="toggle-icon">▼</span>' : ''}
    </div>
    ${hasContent ? `<div class="collapsible-content"><pre>${escapeHTML(content)}</pre></div>` : ''}
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
    <div class="label">Error</div>
    <pre>${escapeHTML(content)}</pre>
  `;
  return div;
}

function appendToCurrentMessage(block) {
  if (!state.currentMessage) return;
  const bubble = state.currentMessage.querySelector('.bubble');
  if (bubble) {
    bubble.appendChild(block);
    scrollToBottom();
  }
}

function setOutputContent(content) {
  if (!state.currentMessage) return;
  const output = state.currentMessage.querySelector('.output-content');
  if (output) {
    output.textContent = content;
    scrollToBottom();
  }
}

function scrollToBottom() {
  els.messages.scrollTop = els.messages.scrollHeight;
}

function renderMessages() {
  const activity = state.currentSession?.activity || [];

  if (!activity.length) {
    els.messages.innerHTML = `
      <div class="welcome-screen">
        <div class="welcome-icon">💬</div>
        <h2>Start a conversation</h2>
        <p>Send a message to begin</p>
      </div>
    `;
    return;
  }

  els.messages.innerHTML = '';
  let currentUserMessage = null;
  let currentAssistantMessage = null;

  for (const entry of activity) {
    if (entry.type === 'prompt') {
      // New user message
      if (currentAssistantMessage) {
        els.messages.appendChild(currentAssistantMessage);
      }
      currentAssistantMessage = null;
      currentUserMessage = createMessageElement('user', entry.prompt);
      els.messages.appendChild(currentUserMessage);
    } else if (entry.type === 'result') {
      // Final result
      if (!currentAssistantMessage) {
        currentAssistantMessage = createMessageElement('assistant');
      }
      const output = currentAssistantMessage.querySelector('.output-content');
      if (output) output.textContent = filterStatusMessages(entry.content) || '';
    } else if (entry.type === 'event' && entry.event) {
      // Events (thinking, tools, errors)
      if (!currentAssistantMessage) {
        currentAssistantMessage = createMessageElement('assistant');
      }
      const bubble = currentAssistantMessage.querySelector('.bubble');
      const event = entry.event;

      switch (event.type) {
        case 'thinking_delta':
        case 'thinking_end':
          let thinkingBlock = bubble.querySelector('.thinking-block');
          const filteredContent = filterStatusMessages(event.content);
          if (!filteredContent) break;

          if (!thinkingBlock) {
            thinkingBlock = createThinkingBlock(filteredContent);
            if (thinkingBlock) bubble.insertBefore(thinkingBlock, bubble.firstChild);
          } else {
            const pre = thinkingBlock.querySelector('.collapsible-content pre') || thinkingBlock.querySelector('pre');
            if (pre) pre.textContent += filteredContent;
          }
          break;

        case 'tool_start':
          // Skip empty tool_start events
          break;

        case 'tool_input':
          const inputBlock = createToolBlock(event.name || 'tool', 'input', event.input);
          if (inputBlock) bubble.appendChild(inputBlock);
          break;

        case 'tool_output':
          const outputBlock = createToolBlock(event.name || 'tool', 'output', event.output);
          if (outputBlock) bubble.appendChild(outputBlock);
          break;

        case 'tool_end':
          // Skip empty tool_end events
          break;

        case 'output_delta':
        case 'output_final':
          const output = currentAssistantMessage.querySelector('.output-content');
          if (output) output.textContent += filterStatusMessages(event.content) || '';
          break;

        case 'error':
          bubble.appendChild(createErrorBlock(event.content || 'Unknown error'));
          break;
      }
    } else if (entry.type === 'error') {
      if (!currentAssistantMessage) {
        currentAssistantMessage = createMessageElement('assistant');
      }
      const bubble = currentAssistantMessage.querySelector('.bubble');
      bubble.appendChild(createErrorBlock(entry.error || 'Unknown error'));
    }
  }

  if (currentAssistantMessage) {
    els.messages.appendChild(currentAssistantMessage);
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
    els.sessionTitle.textContent = getSessionTitle(session);
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
        // Add activity to current session
        state.currentSession.activity = [...(state.currentSession.activity || []), activity];

        // Handle real-time updates
        if (activity.type === 'prompt') {
          // New user message - clear and re-render
          renderMessages();
        } else if (activity.type === 'event' && activity.event) {
          // Append event to current message
          handleStreamEvent(activity.event);
        } else if (activity.type === 'result') {
          // Final result
          setOutputContent(filterStatusMessages(activity.content) || '');
        } else if (activity.type === 'error') {
          appendToCurrentMessage(createErrorBlock(activity.error || 'Unknown error'));
        }
      }
      return;
    }
  });
}

function handleStreamEvent(event) {
  // Ensure we have a current message
  if (!state.currentMessage || !els.messages.contains(state.currentMessage)) {
    state.currentMessage = createMessageElement('assistant');
    els.messages.appendChild(state.currentMessage);
  }

  const bubble = state.currentMessage.querySelector('.bubble');

  switch (event.type) {
    case 'thinking_delta':
      let thinkingBlock = bubble.querySelector('.thinking-block');
      const filteredContent = filterStatusMessages(event.content);
      if (!filteredContent) break;

      if (!thinkingBlock) {
        thinkingBlock = createThinkingBlock(filteredContent);
        if (thinkingBlock) bubble.insertBefore(thinkingBlock, bubble.querySelector('.output-content') || bubble.firstChild);
      } else {
        const pre = thinkingBlock.querySelector('.collapsible-content pre') || thinkingBlock.querySelector('pre');
        if (pre) pre.textContent += filteredContent;
      }
      break;

    case 'thinking_end':
      // Thinking complete
      break;

    case 'tool_start':
      // Skip empty tool_start events
      break;

    case 'tool_input':
      const inputBlock = createToolBlock(event.name || 'tool', 'input', event.input);
      if (inputBlock) bubble.appendChild(inputBlock);
      break;

    case 'tool_output':
      const outputBlock = createToolBlock(event.name || 'tool', 'output', event.output);
      if (outputBlock) bubble.appendChild(outputBlock);
      break;

    case 'tool_end':
      // Skip empty tool_end events
      break;

    case 'output_delta':
      const output = state.currentMessage.querySelector('.output-content');
      if (output) output.textContent += filterStatusMessages(event.content) || '';
      break;

    case 'output_final':
      setOutputContent(filterStatusMessages(event.content) || '');
      break;

    case 'error':
      appendToCurrentMessage(createErrorBlock(event.content || 'Unknown error'));
      break;
  }

  scrollToBottom();
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
    els.sendPrompt.disabled = true;

    // Add user message to UI immediately
    const userMsg = createMessageElement('user', content);
    els.messages.appendChild(userMsg);

    // Create placeholder for assistant response
    state.currentMessage = createMessageElement('assistant');
    els.messages.appendChild(state.currentMessage);

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

    // Reload session to get final state
    await loadSession(state.selectedSessionId);
  } catch (error) {
    console.error('Failed to send prompt:', error);
    appendToCurrentMessage(createErrorBlock(error.message));
  } finally {
    els.sendPrompt.disabled = false;
    els.promptInput.focus();
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
