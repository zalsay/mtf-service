const crypto = require('crypto');
const https = require('https');
const http = require('http');
const { execFile } = require('child_process');
const {
  buildChatCompletionsBody,
  normalizeMessages,
  parseChatCompletionMessage,
} = require('./deepseek_tui_runtime_lib');

const host = process.env.DEEPSEEK_TUI_HOST || '0.0.0.0';
const port = Number(process.env.DEEPSEEK_TUI_PORT || '7878');
const defaultModel = process.env.DEEPSEEK_TUI_DEFAULT_MODEL || 'deepseek-v4-pro';
const cliBin = process.env.DEEPSEEK_TUI_CLI_BIN || 'deepseek';
const timeoutMs = Number(process.env.DEEPSEEK_TUI_CLI_TIMEOUT_SECONDS || '900') * 1000;
const maxContextMessages = Number(process.env.DEEPSEEK_TUI_MAX_CONTEXT_MESSAGES || '12');

const threads = new Map();

function sendJSON(res, status, payload) {
  const body = JSON.stringify(payload);
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(body),
  });
  res.end(body);
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    req.on('data', (chunk) => {
      size += chunk.length;
      if (size > 4 * 1024 * 1024) {
        reject(new Error('request body too large'));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on('end', () => {
      const raw = Buffer.concat(chunks).toString('utf8').trim();
      if (!raw) {
        resolve({});
        return;
      }
      try {
        resolve(JSON.parse(raw));
      } catch (error) {
        reject(new Error('invalid json body'));
      }
    });
    req.on('error', reject);
  });
}

function tokenFromAuthorization(headerValue) {
  const value = String(headerValue || '').trim();
  const match = value.match(/^Bearer\s+(.+)$/i);
  return match ? match[1].trim() : '';
}

function requestAIModel(req, payload, fallback) {
  const bodyModel = payload && typeof payload.ai_model === 'object' ? payload.ai_model : {};
  const headerKey = req.headers['x-deepseek-api-key'] || tokenFromAuthorization(req.headers.authorization);
  const headerBaseURL = req.headers['x-deepseek-base-url'];
  return {
    provider_name: String(bodyModel.provider_name || fallback?.provider_name || 'DeepSeek').trim(),
    base_url: String(bodyModel.base_url || headerBaseURL || fallback?.base_url || process.env.DEEPSEEK_BASE_URL || '').trim(),
    api_key: String(bodyModel.api_key || headerKey || fallback?.api_key || process.env.DEEPSEEK_API_KEY || '').trim(),
    model_id: String(bodyModel.model_id || fallback?.model_id || payload?.model || defaultModel).trim(),
  };
}

function buildEnv(aiModel) {
  const env = { ...process.env };
  if (aiModel.api_key) {
    env.DEEPSEEK_API_KEY = aiModel.api_key;
    env.OPENAI_API_KEY = aiModel.api_key;
  }
  if (aiModel.base_url) {
    const baseURL = aiModel.base_url.replace(/\/+$/, '');
    env.DEEPSEEK_BASE_URL = baseURL;
    env.OPENAI_BASE_URL = baseURL;
  }
  return env;
}

function stripAnsi(value) {
  return String(value || '').replace(/\x1B\[[0-?]*[ -/]*[@-~]/g, '').trim();
}

function chatCompletionsURL(aiModel) {
  const rawBase = String(aiModel.base_url || process.env.DEEPSEEK_BASE_URL || 'https://api.deepseek.com').trim().replace(/\/+$/, '');
  if (!rawBase) {
    return 'https://api.deepseek.com/chat/completions';
  }
  if (/\/chat\/completions$/i.test(rawBase)) {
    return rawBase;
  }
  return `${rawBase}/chat/completions`;
}

function writeSSE(res, event, payload) {
  res.write(`event: ${event}\n`);
  res.write(`data: ${JSON.stringify(payload)}\n\n`);
}

function postChatCompletions(aiModel, bodyPayload) {
  return new Promise((resolve, reject) => {
    if (!aiModel.api_key) {
      reject(new Error('DeepSeek API key not found.'));
      return;
    }

    const endpoint = new URL(chatCompletionsURL(aiModel));
    const client = endpoint.protocol === 'http:' ? http : https;
    const body = JSON.stringify(bodyPayload);

    const req = client.request({
      method: 'POST',
      protocol: endpoint.protocol,
      hostname: endpoint.hostname,
      port: endpoint.port || undefined,
      path: `${endpoint.pathname}${endpoint.search}`,
      headers: {
        Authorization: `Bearer ${aiModel.api_key}`,
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(body),
      },
      timeout: timeoutMs,
    }, (upstream) => {
      resolve(upstream);
    });

    req.on('timeout', () => req.destroy(new Error('DeepSeek API timed out')));
    req.on('error', reject);
    req.write(body);
    req.end();
  });
}

function streamDeepSeek(prompt, aiModel, res, payload = {}) {
  return new Promise(async (resolve, reject) => {
    const bodyPayload = buildChatCompletionsBody({
      model: aiModel.model_id || defaultModel,
      messages: payload.messages,
      prompt,
      tools: payload.tools,
      toolChoice: payload.tool_choice,
      stream: true,
    });
    if (bodyPayload.messages.length === 0) {
      reject(new Error('prompt is required'));
      return;
    }

    let upstream;
    try {
      upstream = await postChatCompletions(aiModel, bodyPayload);
    } catch (error) {
      reject(error);
      return;
    }
    if (upstream.statusCode >= 400) {
      const chunks = [];
      upstream.on('data', (chunk) => chunks.push(chunk));
      upstream.on('end', () => {
        const detail = Buffer.concat(chunks).toString('utf8').trim() || `status ${upstream.statusCode}`;
        reject(new Error(`DeepSeek API stream failed: ${detail}`));
      });
      return;
    }

    let buffer = '';
    upstream.setEncoding('utf8');
    upstream.on('data', (chunk) => {
      buffer += chunk.replace(/\r\n/g, '\n');
      let boundary = buffer.indexOf('\n\n');
      while (boundary >= 0) {
        const block = buffer.slice(0, boundary);
        buffer = buffer.slice(boundary + 2);
        for (const line of block.split('\n')) {
          const trimmed = line.trim();
          if (!trimmed.startsWith('data:')) continue;
          const data = trimmed.slice(5).trim();
          if (!data || data === '[DONE]') {
            if (data === '[DONE]') writeSSE(res, 'done', { done: true });
            continue;
          }
          try {
            const decoded = JSON.parse(data);
            const text = decoded?.choices?.[0]?.delta?.content || decoded?.choices?.[0]?.text || '';
            if (text) writeSSE(res, 'delta', { text });
          } catch {
            // Ignore malformed upstream keepalive lines.
          }
        }
        boundary = buffer.indexOf('\n\n');
      }
    });
    upstream.on('end', () => resolve());
    upstream.on('error', reject);
  });
}

async function runDeepSeekChatCompletion(payload, aiModel) {
  const bodyPayload = buildChatCompletionsBody({
    model: aiModel.model_id || defaultModel,
    messages: payload.messages,
    prompt: payload.prompt,
    tools: payload.tools,
    toolChoice: payload.tool_choice,
    stream: false,
  });
  if (bodyPayload.messages.length === 0) {
    throw new Error('prompt is required');
  }
  const upstream = await postChatCompletions(aiModel, bodyPayload);
  const chunks = [];
  upstream.on('data', (chunk) => chunks.push(chunk));
  await new Promise((resolve, reject) => {
    upstream.on('end', resolve);
    upstream.on('error', reject);
  });
  const detail = Buffer.concat(chunks).toString('utf8').trim();
  if (upstream.statusCode >= 400) {
    throw new Error(`DeepSeek API failed: ${detail || `status ${upstream.statusCode}`}`);
  }
  const decoded = detail ? JSON.parse(detail) : {};
  return parseChatCompletionMessage(decoded);
}

function runDeepSeek(prompt, aiModel) {
  return new Promise((resolve, reject) => {
    if (!aiModel.api_key) {
      reject(new Error('DeepSeek API key not found.'));
      return;
    }
    const model = aiModel.model_id || defaultModel;
    const args = ['--model', model, '--skip-onboarding', '-p', prompt];
    execFile(cliBin, args, {
      env: buildEnv(aiModel),
      timeout: timeoutMs,
      maxBuffer: 8 * 1024 * 1024,
    }, (error, stdout, stderr) => {
      const cleanStdout = stripAnsi(stdout);
      const cleanStderr = stripAnsi(stderr);
      if (error) {
        const detail = cleanStderr || cleanStdout || error.message;
        reject(new Error(`Failed to send message: ${detail}`));
        return;
      }
      resolve(cleanStdout || cleanStderr || '');
    });
  });
}

function threadPrompt(thread, userPrompt) {
  const parts = [];
  if (thread.system_prompt) {
    parts.push(`System:\n${thread.system_prompt}`);
  }
  const recent = thread.messages.slice(-maxContextMessages);
  for (const message of recent) {
    parts.push(`${message.role === 'assistant' ? 'Assistant' : 'User'}:\n${message.content}`);
  }
  parts.push(`User:\n${userPrompt}`);
  return parts.join('\n\n');
}

function createThread(payload, aiModel) {
  const id = `thr_${crypto.randomBytes(4).toString('hex')}`;
  const now = new Date().toISOString();
  const thread = {
    id,
    title: String(payload.title || 'MTF Agent').trim(),
    model: aiModel.model_id || payload.model || defaultModel,
    system_prompt: String(payload.system_prompt || '').trim(),
    ai_model: aiModel,
    messages: [],
    turns: [],
    items: [],
    created_at: now,
    updated_at: now,
  };
  threads.set(id, thread);
  return thread;
}

async function handleCreateThread(req, res) {
  const payload = await readBody(req);
  const aiModel = requestAIModel(req, payload, null);
  const thread = createThread(payload, aiModel);
  sendJSON(res, 201, {
    id: thread.id,
    thread: { id: thread.id, title: thread.title, model: thread.model },
  });
}

async function handleCreateTurn(req, res, threadID) {
  const thread = threads.get(threadID);
  if (!thread) {
    sendJSON(res, 404, { error: { message: `Thread not found: ${threadID}`, status: 404 } });
    return;
  }
  const payload = await readBody(req);
  const prompt = String(payload.prompt || payload.message || '').trim();
  const messages = normalizeMessages(payload.messages, prompt);
  if (!prompt && messages.length === 0) {
    sendJSON(res, 400, { error: { message: 'prompt is required', status: 400 } });
    return;
  }
  const aiModel = requestAIModel(req, payload, thread.ai_model);
  const turnID = `turn_${crypto.randomBytes(4).toString('hex')}`;
  const useStandardChat = Array.isArray(payload.tools) && payload.tools.length > 0 || Array.isArray(payload.messages);
  const assistant = useStandardChat
    ? await runDeepSeekChatCompletion({
      messages: messages.length > 0 ? messages : [{ role: 'user', content: threadPrompt(thread, prompt) }],
      tools: payload.tools,
      tool_choice: payload.tool_choice,
    }, aiModel)
    : { content: await runDeepSeek(threadPrompt(thread, prompt), aiModel), tool_calls: [] };
  const now = new Date().toISOString();
  thread.ai_model = aiModel;
  thread.messages.push({ role: 'user', content: prompt, turn_id: turnID, created_at: now });
  thread.messages.push({ role: 'assistant', content: assistant.content, reasoning_content: assistant.reasoning_content, tool_calls: assistant.tool_calls, turn_id: turnID, created_at: now });
  thread.turns.push({ id: turnID, status: 'completed', created_at: now, updated_at: now });
  thread.items.push({ turn_id: turnID, kind: 'assistant_message', status: 'completed', detail: assistant.content, reasoning_content: assistant.reasoning_content, tool_calls: assistant.tool_calls });
  thread.updated_at = now;
  sendJSON(res, 201, { turn: { id: turnID, status: 'completed' }, message: assistant.content, reasoning_content: assistant.reasoning_content, tool_calls: assistant.tool_calls });
}

function handleGetThread(res, threadID) {
  const thread = threads.get(threadID);
  if (!thread) {
    sendJSON(res, 404, { error: { message: `Thread not found: ${threadID}`, status: 404 } });
    return;
  }
  sendJSON(res, 200, {
    id: thread.id,
    model: thread.model,
    turns: thread.turns,
    items: thread.items,
    messages: thread.messages,
  });
}

async function handleStream(req, res) {
  const payload = await readBody(req);
  const aiModel = requestAIModel(req, payload, null);
  const prompt = String(payload.prompt || payload.message || '').trim();
  const messages = normalizeMessages(payload.messages, prompt);
  if (!prompt && messages.length === 0) {
    sendJSON(res, 400, { error: { message: 'prompt is required', status: 400 } });
    return;
  }
  res.writeHead(200, {
    'Content-Type': 'text/event-stream; charset=utf-8',
    'Cache-Control': 'no-cache, no-transform',
    'X-Accel-Buffering': 'no',
    Connection: 'keep-alive',
  });
  try {
    await streamDeepSeek(prompt, aiModel, res, payload);
  } catch (error) {
    writeSSE(res, 'error', { error: error.message || String(error) });
  } finally {
    res.end();
  }
}

function route(req) {
  const url = new URL(req.url, 'http://localhost');
  const path = url.pathname.replace(/\/+$/, '') || '/';
  const turnMatch = path.match(/^\/v1\/threads\/([^/]+)\/turns$/);
  const threadMatch = path.match(/^\/v1\/threads\/([^/]+)$/);
  return { path, turnID: turnMatch && decodeURIComponent(turnMatch[1]), threadID: threadMatch && decodeURIComponent(threadMatch[1]) };
}

const server = http.createServer(async (req, res) => {
  try {
    const current = route(req);
    if (req.method === 'GET' && current.path === '/health') {
      sendJSON(res, 200, { status: 'ok', runtime: 'deepseek-tui-cli-wrapper' });
    } else if (req.method === 'POST' && current.path === '/v1/threads') {
      await handleCreateThread(req, res);
    } else if (req.method === 'POST' && current.turnID) {
      await handleCreateTurn(req, res, current.turnID);
    } else if (req.method === 'GET' && current.threadID) {
      handleGetThread(res, current.threadID);
    } else if (req.method === 'POST' && current.path === '/v1/stream') {
      await handleStream(req, res);
    } else {
      sendJSON(res, 404, { error: { message: 'not found', status: 404 } });
    }
  } catch (error) {
    sendJSON(res, 500, { error: { message: error.message || String(error), status: 500 } });
  }
});

server.listen(port, host, () => {
  console.log(`DeepSeek-TUI CLI wrapper listening on ${host}:${port}`);
});
