import { PublicPredictionResponse, StrategyParams } from '../types';

// API服务 - 连接fintrack-api后端
// Vite 会根据运行模式自动加载对应的 .env 文件:
// - npm run dev -> .env.development
// - npm run build -> .env.production
const normalizeAPIBaseURL = (value?: string) => {
  const trimmed = (value || '/api/v1').trim().replace(/\/+$/, '');
  return /\/api\/v1$/i.test(trimmed) ? trimmed : `${trimmed}/api/v1`;
};

const API_BASE_URL = normalizeAPIBaseURL((import.meta as any).env.VITE_API_BASE_URL);
const buildAPIURL = (endpoint: string) => `${API_BASE_URL}/${endpoint.replace(/^\/+/, '')}`;

const buildWebSocketURL = (endpoint: string) => {
  const apiBaseURL = new URL(API_BASE_URL.endsWith('/') ? API_BASE_URL : `${API_BASE_URL}/`, window.location.href);
  const path = `${apiBaseURL.pathname.replace(/\/+$/, '')}/${endpoint.replace(/^\/+/, '')}`;
  apiBaseURL.pathname = path;
  apiBaseURL.protocol = apiBaseURL.protocol === 'https:' ? 'wss:' : 'ws:';
  return apiBaseURL;
};

// 存储认证token
let authToken: string | null = null;

// API响应类型定义
export interface User {
  id: number;
  email: string;
  username: string;
  first_name?: string;
  last_name?: string;
  is_premium: boolean;
  is_admin?: boolean;
  membership_level?: number;
  membership_expires_at?: string | null;
  created_at: string;
  updated_at?: string;
}

export interface AuthResponse {
  token: string;
  user: User;
}

export interface RegisterOptions {
  captchaVerifyParam?: string;
  activationCode?: string;
}

export interface WatchlistItem {
  id: number;
  stock: {
    id: number;
    symbol: string;
    company_name: string;
    exchange: string;
    sector: string;
    industry?: string;
    market_cap?: number;
    created_at: string;
    updated_at: string;
  };
  current_price: {
    id: number;
    stock_id: number;
    price: number;
    change_percent: number;
    volume: number;
    market_cap?: number;
    recorded_at: string;
  };
  added_at: string;
  notes?: string;
  unique_key?: string;
  stock_type?: number;
  strategy_unique_key?: string;
  strategy_name?: string;
  is_over_limit?: boolean;
  watchlist_limit?: number;
}

export interface WatchlistResponse {
  count: number;
  watchlist: WatchlistItem[];
}

export interface AddToWatchlistRequest {
  symbol: string;
  stock_type?: number;
}

export interface UZIReportItem {
  id?: number;
  ticker: string;
  depth?: 'lite' | 'medium' | 'deep' | string;
  status?: string;
  directory_name: string;
  date_tag: string;
  report_relative_path: string;
  report_url: string;
  size_bytes: number;
  updated_at: string;
}

export interface UZIReportListResponse {
  items: UZIReportItem[];
  count: number;
}

export interface UZIReportOpenTokenResponse {
  open_token: string;
  expires_in: number;
  open_url: string;
}

export type FinanceNewsCategory = 'market' | 'global' | 'stock' | 'announcements' | 'lhb' | 'hot_etf';

export interface FinanceNewsItem {
  id: string;
  title: string;
  summary?: string;
  published_at?: string;
  source?: string;
  url?: string;
  symbol?: string;
  stock_name?: string;
  category?: string;
  etf_rps?: string;
  etf_month?: string;
  etf_week?: string;
  etf_day?: string;
  etf_stop_loss?: string;
  etf_score?: string;
  etf_status?: string;
}

export interface FinanceNewsResponse {
  status: string;
  source: string;
  category: FinanceNewsCategory | string;
  query: {
    category: string;
    symbol?: string;
    keyword?: string;
    limit: number;
    page: number;
  };
  count: number;
  items: FinanceNewsItem[];
}

export interface HotETFSignal {
  score: number;
  text: string;
}

export interface HotETFItem {
  code: string;
  name: string;
  risk_rps: number;
  radar_priority: number;
  grade?: string;
  trend?: string;
  month: HotETFSignal;
  week: HotETFSignal;
  day: HotETFSignal;
  stop_price?: string;
  stop_distance?: string;
  total_score: number;
  status: string;
}

export interface HotETFResponse {
  status: string;
  source: string;
  count: number;
  items: HotETFItem[];
}

export interface UZIAnalyzeRequest {
  ticker: string;
  depth?: 'lite' | 'medium' | 'deep';
  no_resume?: boolean;
}

export interface UZIHealthResponse {
  status: string;
  service?: string;
  runtime_root?: string;
  reports_root?: string;
  port?: number;
  timeout_seconds?: number;
  run_py_exists?: boolean;
  error?: string;
}

export interface UZIAnalyzeResponse {
  status: string;
  ticker: string;
  exit_code: number;
  duration_seconds: number;
  report_path?: string;
  report_relative_path?: string;
  report_url?: string;
  report?: UZIReportItem;
  stdout_tail?: string;
  stderr_tail?: string;
}

export interface UZIAnalyzeStatus {
  status: 'idle' | 'running' | 'processing' | 'completed' | 'failed' | string;
  job_id?: string;
  ticker?: string;
  stage?: string;
  summary?: string;
  report?: UZIReportItem;
  started_at?: string;
  updated_at?: string;
}

export interface UZIAnalyzeQueueResponse {
  success: boolean;
  message?: string;
  reused?: boolean;
  force_enqueue?: boolean;
  job_id: string;
  job_kind?: string;
  status: 'queued' | 'running' | 'succeeded' | 'failed' | string;
  ticker?: string;
  current_stage?: string;
  target_path?: string;
  request_key?: string;
  created_at?: string;
  status_url?: string;
  queue_position?: number;
  queue_status?: unknown;
}

export interface UZIAnalyzeJobStatusResponse {
  job_id: string;
  job_kind?: string;
  status: 'queued' | 'running' | 'succeeded' | 'failed' | string;
  force_enqueue?: boolean;
  ticker?: string;
  stock_code?: string;
  current_stage?: string;
  target_path?: string;
  backend?: string;
  upstream_status?: number;
  error?: string;
  queue_position?: number;
  created_at?: string;
  started_at?: string;
  finished_at?: string;
  result?: UZIAnalyzeResponse;
  report?: UZIReportItem;
}

export type UZIAnalyzeStage =
  | 'bootstrap'
  | 'market_data'
  | 'analysis'
  | 'reporting'
  | 'finalizing'
  | string;

export interface UZIAnalyzeStartPayload {
  status?: string;
  ticker?: string;
  summary?: string;
  started_at?: string;
}

export interface UZIAnalyzeStagePayload {
  status?: string;
  ticker?: string;
  stage: UZIAnalyzeStage;
  summary?: string;
}

export interface UZIAnalyzeLogPayload {
  status?: string;
  ticker?: string;
  stream?: string;
  line?: string;
}

export interface UZIAnalyzeErrorPayload {
  status?: string;
  ticker?: string;
  error?: string;
  exit_code?: number;
  duration_seconds?: number;
  stdout_tail?: string;
  stderr_tail?: string;
}

export interface UZIAnalyzeStartEvent {
  event: 'start';
  data: UZIAnalyzeStartPayload;
}

export interface UZIAnalyzeStageEvent {
  event: 'stage';
  data: UZIAnalyzeStagePayload;
}

export interface UZIAnalyzeLogEvent {
  event: 'log';
  data: UZIAnalyzeLogPayload;
}

export interface UZIAnalyzeCompleteEvent {
  event: 'complete';
  data: UZIAnalyzeResponse;
}

export interface UZIAnalyzeErrorEvent {
  event: 'error';
  data: UZIAnalyzeErrorPayload;
}

export type UZIAnalyzeStreamEvent =
  | UZIAnalyzeStartEvent
  | UZIAnalyzeStageEvent
  | UZIAnalyzeLogEvent
  | UZIAnalyzeCompleteEvent
  | UZIAnalyzeErrorEvent;

export interface UZIAnalyzeStreamHandlers {
  onEvent?: (event: UZIAnalyzeStreamEvent) => void;
  onStart?: (event: UZIAnalyzeStartEvent) => void;
  onStage?: (event: UZIAnalyzeStageEvent) => void;
  onLog?: (event: UZIAnalyzeLogEvent) => void;
  onComplete?: (event: UZIAnalyzeCompleteEvent) => void;
  onError?: (event: UZIAnalyzeErrorEvent) => void;
  signal?: AbortSignal;
}

export interface AIModelConfig {
  id?: number;
  provider_name: string;
  display_name: string;
  base_url: string;
  api_key?: string;
  api_key_masked?: string;
  has_api_key: boolean;
  model_id: string;
  is_recommended: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface AIModelConfigRequest {
  base_url: string;
  api_key?: string;
  model_id: string;
}

export interface MTFAgentSession {
  thread_id?: string;
  model_id?: string;
  runtime_available: boolean;
  memory_count: number;
  has_ai_model_config: boolean;
}

export interface MTFAgentMemory {
  id: number;
  memory_type: string;
  content: string;
  source: string;
  confidence: number;
  created_at: string;
  updated_at: string;
}

export interface MTFAgentMessage {
  id?: number;
  thread_id?: string;
  role: 'user' | 'assistant' | 'system';
  content: string;
  created_at?: string;
}

export interface MTFAgentMessageResponse {
  thread_id: string;
  message: MTFAgentMessage;
  model?: string;
}

export interface MTFAgentMessageJobResponse {
  job_id: string;
  status: 'queued' | 'running' | 'completed' | 'failed';
}

export interface MTFAgentMessageJobStatusResponse {
  job_id: string;
  status: 'queued' | 'running' | 'completed' | 'failed';
  response?: MTFAgentMessageResponse;
  error?: string;
}

export interface MTFAgentMessagesResponse {
  thread_id?: string;
  messages: MTFAgentMessage[];
}

interface MTFAgentStreamErrorPayload {
  error?: string;
}

interface MTFAgentStreamDeltaPayload {
  text?: string;
}

export interface MTFAgentSendOptions {
  onDelta?: (text: string) => void;
}

export interface MembershipInviteCode {
  id: number;
  code: string;
  membership_level: number;
  duration_days: number;
  is_active: boolean;
  used_count: number;
  max_uses: number;
  note?: string | null;
  created_by?: number | null;
  created_at: string;
  updated_at: string;
}

export interface CreateMembershipInviteRequest {
  code?: string;
  membership_level: number;
  duration_days: number;
  max_uses?: number;
  is_active?: boolean;
  note?: string | null;
}

export interface RedeemInviteResponse {
  message: string;
  membership_level: number;
  membership_expires_at: string;
}

// 设置认证token
export const setAuthToken = (token: string) => {
  authToken = token;
  localStorage.setItem('authToken', token);
};

// 获取认证token
export const getAuthToken = (): string | null => {
  if (!authToken) {
    authToken = localStorage.getItem('authToken');
  }
  return authToken;
};

// 清除认证token
export const clearAuthToken = () => {
  authToken = null;
  localStorage.removeItem('authToken');
};

// 通用API请求函数
const apiRequest = async (endpoint: string, options: RequestInit = {}): Promise<any> => {
  const url = buildAPIURL(endpoint);
  const token = getAuthToken();

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  };

  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const response = await fetch(url, {
    ...options,
    headers,
  });

  if (!response.ok) {
    const errorData = await response.json().catch(() => ({}));
    throw new Error(errorData.message || errorData.error || `HTTP error! status: ${response.status}`);
  }

  return response.json();
};

const apiFetch = async (endpoint: string, options: RequestInit = {}): Promise<Response> => {
  const url = buildAPIURL(endpoint);
  const token = getAuthToken();
  const headers: Record<string, string> = {
    ...(options.headers as Record<string, string>),
  };

  if (!('Content-Type' in headers) && options.body && !(options.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json';
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  return fetch(url, {
    ...options,
    headers,
  });
};

const resolveOpenURL = (openURL: string, locationHref: string = window.location.href): string => {
  const trimmedURL = openURL.trim();
  if (/^https?:\/\//i.test(trimmedURL)) {
    return trimmedURL;
  }

  const apiBaseURL = new URL(API_BASE_URL.endsWith('/') ? API_BASE_URL : `${API_BASE_URL}/`, locationHref);
  if (trimmedURL.startsWith('/api/')) {
    const normalizedPath = apiBaseURL.pathname.replace(/\/+$/, '');
    const apiPrefixIndex = normalizedPath.indexOf('/api/');
    const deployPrefix = apiPrefixIndex >= 0 ? normalizedPath.slice(0, apiPrefixIndex) : '';
    return new URL(`${deployPrefix}${trimmedURL}`, apiBaseURL.origin).toString();
  }

  return new URL(trimmedURL.replace(/^\/+/, ''), apiBaseURL).toString();
};

const readAPIError = async (response: Response): Promise<string> => {
  try {
    const errorData = await response.json();
    return errorData.error || `HTTP error! status: ${response.status}`;
  } catch {
    return `HTTP error! status: ${response.status}`;
  }
};

const parseSSEBlock = (block: string): { event: string; data: string } | null => {
  const trimmed = block.trim();
  if (!trimmed) {
    return null;
  }

  let event = 'message';
  const dataLines: string[] = [];

  for (const line of trimmed.split('\n')) {
    if (line.startsWith('event:')) {
      event = line.slice(6).trim();
      continue;
    }
    if (line.startsWith('data:')) {
      const dataLine = line.slice(5);
      dataLines.push(dataLine.startsWith(' ') ? dataLine.slice(1) : dataLine);
    }
  }

  return {
    event,
    data: dataLines.join('\n'),
  };
};

const parseSSEJSON = <T>(raw: string): T => {
  return JSON.parse(raw) as T;
};

const dispatchUZIAnalyzeEvent = (handlers: UZIAnalyzeStreamHandlers, event: UZIAnalyzeStreamEvent) => {
  handlers.onEvent?.(event);
  switch (event.event) {
    case 'start':
      handlers.onStart?.(event);
      return;
    case 'stage':
      handlers.onStage?.(event);
      return;
    case 'log':
      handlers.onLog?.(event);
      return;
    case 'complete':
      handlers.onComplete?.(event);
      return;
    case 'error':
      handlers.onError?.(event);
      return;
  }
};

const consumeUZIAnalyzeStream = async (response: Response, handlers: UZIAnalyzeStreamHandlers = {}): Promise<UZIAnalyzeResponse> => {
  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  const contentType = (response.headers.get('Content-Type') || '').toLowerCase();
  if (!contentType.includes('text/event-stream')) {
    throw new Error(`Unexpected UZI analyze response content-type: ${contentType || 'unknown'}`);
  }
  if (!response.body) {
    throw new Error('UZI analyze stream is not readable');
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let completed: UZIAnalyzeResponse | null = null;
  let streamError: string | null = null;

  const handleBlock = (rawBlock: string) => {
    const parsed = parseSSEBlock(rawBlock);
    if (!parsed) {
      return;
    }

    switch (parsed.event) {
      case 'start': {
        const event: UZIAnalyzeStartEvent = {
          event: 'start',
          data: parseSSEJSON<UZIAnalyzeStartPayload>(parsed.data),
        };
        dispatchUZIAnalyzeEvent(handlers, event);
        return;
      }
      case 'stage': {
        const event: UZIAnalyzeStageEvent = {
          event: 'stage',
          data: parseSSEJSON<UZIAnalyzeStagePayload>(parsed.data),
        };
        dispatchUZIAnalyzeEvent(handlers, event);
        return;
      }
      case 'log': {
        const event: UZIAnalyzeLogEvent = {
          event: 'log',
          data: parseSSEJSON<UZIAnalyzeLogPayload>(parsed.data),
        };
        dispatchUZIAnalyzeEvent(handlers, event);
        return;
      }
      case 'complete': {
        const event: UZIAnalyzeCompleteEvent = {
          event: 'complete',
          data: parseSSEJSON<UZIAnalyzeResponse>(parsed.data),
        };
        completed = event.data;
        dispatchUZIAnalyzeEvent(handlers, event);
        return;
      }
      case 'error': {
        const event: UZIAnalyzeErrorEvent = {
          event: 'error',
          data: parseSSEJSON<UZIAnalyzeErrorPayload>(parsed.data),
        };
        streamError = event.data.error || 'UZI analyze failed';
        dispatchUZIAnalyzeEvent(handlers, event);
        return;
      }
      default:
        return;
    }
  };

  while (true) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value || new Uint8Array(), { stream: !done }).replace(/\r\n/g, '\n');

    let boundaryIndex = buffer.indexOf('\n\n');
    while (boundaryIndex >= 0) {
      const block = buffer.slice(0, boundaryIndex);
      buffer = buffer.slice(boundaryIndex + 2);
      handleBlock(block);
      boundaryIndex = buffer.indexOf('\n\n');
    }

    if (done) {
      break;
    }
  }

  const finalChunk = decoder.decode();
  if (finalChunk) {
    buffer += finalChunk.replace(/\r\n/g, '\n');
  }
  if (buffer.trim()) {
    handleBlock(buffer);
  }

  if (completed) {
    return completed;
  }
  if (streamError) {
    throw new Error(streamError);
  }
  throw new Error('UZI analyze stream closed before completion');
};

const consumeMTFAgentMessageStream = async (
  response: Response,
  options: MTFAgentSendOptions = {}
): Promise<MTFAgentMessageResponse> => {
  if (!response.ok) {
    throw new Error(await readAPIError(response));
  }

  const contentType = (response.headers.get('Content-Type') || '').toLowerCase();
  if (!contentType.includes('text/event-stream')) {
    throw new Error(`Unexpected MTF Agent response content-type: ${contentType || 'unknown'}`);
  }
  if (!response.body) {
    throw new Error('MTF Agent stream is not readable');
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let completed: MTFAgentMessageResponse | null = null;
  let streamError: string | null = null;

  const handleBlock = (rawBlock: string) => {
    const parsed = parseSSEBlock(rawBlock);
    if (!parsed) return;

    if (parsed.event === 'done') {
      completed = parseSSEJSON<MTFAgentMessageResponse>(parsed.data);
      return;
    }
    if (parsed.event === 'delta') {
      const payload = parseSSEJSON<MTFAgentStreamDeltaPayload>(parsed.data);
      if (payload.text) options.onDelta?.(payload.text);
      return;
    }
    if (parsed.event === 'error') {
      const payload = parseSSEJSON<MTFAgentStreamErrorPayload>(parsed.data);
      streamError = payload.error || 'MTF Agent request failed';
    }
  };

  while (true) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value || new Uint8Array(), { stream: !done }).replace(/\r\n/g, '\n');

    let boundaryIndex = buffer.indexOf('\n\n');
    while (boundaryIndex >= 0) {
      const block = buffer.slice(0, boundaryIndex);
      buffer = buffer.slice(boundaryIndex + 2);
      handleBlock(block);
      boundaryIndex = buffer.indexOf('\n\n');
    }

    if (done) break;
  }

  const finalChunk = decoder.decode();
  if (finalChunk) {
    buffer += finalChunk.replace(/\r\n/g, '\n');
  }
  if (buffer.trim()) {
    handleBlock(buffer);
  }

  if (completed) return completed;
  if (streamError) throw new Error(streamError);
  throw new Error('MTF Agent stream closed before completion');
};

const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));

const waitMTFAgentMessageJob = async (jobID: string): Promise<MTFAgentMessageResponse> => {
  const maxAttempts = 120;
  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    if (attempt > 0) await sleep(2000);
    const status = await apiRequest<MTFAgentMessageJobStatusResponse>(`/mtf-agent/messages/jobs/${encodeURIComponent(jobID)}`, { method: 'GET' });
    if (status.status === 'completed' && status.response) {
      return status.response;
    }
    if (status.status === 'failed') {
      throw new Error(status.error || 'MTF Agent request failed');
    }
  }
  throw new Error('MTF Agent request timed out');
};

const normalizeWatchlistItem = (raw: any): WatchlistItem | null => {
  if (!raw || typeof raw !== 'object') {
    return null;
  }

  const stock = raw.stock;
  if (!stock || typeof stock !== 'object' || !stock.symbol) {
    return null;
  }

  return {
    ...raw,
    stock: {
      id: Number(stock.id || 0),
      symbol: String(stock.symbol),
      company_name: String(stock.company_name || ''),
      exchange: String(stock.exchange || ''),
      sector: String(stock.sector || ''),
      industry: stock.industry ? String(stock.industry) : undefined,
      market_cap: typeof stock.market_cap === 'number' ? stock.market_cap : undefined,
      created_at: String(stock.created_at || ''),
      updated_at: String(stock.updated_at || ''),
    },
    current_price: raw.current_price && typeof raw.current_price === 'object'
      ? raw.current_price
      : undefined,
  };
};

// 认证API
export const authAPI = {
  // 用户注册
  register: async (email: string, username: string, password: string, options: RegisterOptions = {}): Promise<AuthResponse> => {
    const headers = options.captchaVerifyParam
      ? { 'captcha-verify-param': options.captchaVerifyParam }
      : undefined;
    const activationCode = options.activationCode?.trim();
    const response = await apiFetch('/auth/register', {
      method: 'POST',
      headers,
      body: JSON.stringify({
        email,
        username,
        password,
        ...(activationCode ? { activation_code: activationCode } : {}),
      }),
    });
    const errorPayload = response.ok ? null : await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(errorPayload.message || errorPayload.error || `HTTP error! status: ${response.status}`);
    }

    const captchaVerifyCode = response.headers.get('X-Captcha-Verify-Code');
    if (captchaVerifyCode && captchaVerifyCode !== 'T001') {
      throw new Error(`Captcha verification failed: ${captchaVerifyCode}`);
    }

    const data = await response.json();
    setAuthToken(data.token);
    return data;
  },

  // 用户登录
  login: async (email: string, password: string): Promise<AuthResponse> => {
    const response = await apiRequest('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    });
    setAuthToken(response.token);
    return response;
  },

  // 获取用户资料
  getProfile: async (): Promise<User> => {
    return apiRequest('/auth/profile');
  },
  updateMembership: async (
    level: number,
    membershipExpiresAt?: string | null
  ): Promise<{ message: string; membership_level: number; membership_expires_at?: string | null }> => {
    return apiRequest('/auth/membership', {
      method: 'PUT',
      body: JSON.stringify({
        membership_level: level,
        membership_expires_at: membershipExpiresAt,
      }),
    });
  },
  redeemInvite: async (code: string): Promise<RedeemInviteResponse> => {
    return apiRequest('/auth/redeem-invite', {
      method: 'POST',
      body: JSON.stringify({ code }),
    });
  },
  // 用户注销
  logout: async (): Promise<void> => {
    await apiRequest('/auth/logout', { method: 'POST' });
    clearAuthToken();
  },
};

export const settingsAPI = {
  getAIModelConfig: async (): Promise<AIModelConfig> => {
    return apiRequest('/settings/ai-model', { method: 'GET' });
  },
  updateAIModelConfig: async (data: AIModelConfigRequest): Promise<AIModelConfig> => {
    return apiRequest('/settings/ai-model', {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },
};

export const mtfAgentAPI = {
  getSession: async (): Promise<MTFAgentSession> => {
    return apiRequest('/mtf-agent/session', { method: 'GET' });
  },
  getMessages: async (): Promise<MTFAgentMessagesResponse> => {
    const response = await apiRequest('/mtf-agent/messages', { method: 'GET' });
    return {
      thread_id: response.thread_id,
      messages: Array.isArray(response.messages) ? response.messages : [],
    };
  },
  sendMessage: async (message: string, options: MTFAgentSendOptions = {}): Promise<MTFAgentMessageResponse> => {
    const response = await apiFetch('/mtf-agent/messages/stream', {
      method: 'POST',
      body: JSON.stringify({ message }),
    });
    return consumeMTFAgentMessageStream(response, options);
  },
  reset: async (): Promise<{ thread_id: string }> => {
    return apiRequest('/mtf-agent/reset', { method: 'POST' });
  },
  getMemory: async (): Promise<{ items: MTFAgentMemory[]; count: number }> => {
    const response = await apiRequest('/mtf-agent/memory', { method: 'GET' });
    const items = Array.isArray(response.memories) ? response.memories : [];
    return { items, count: items.length };
  },
  clearMemory: async (): Promise<{ message: string }> => {
    return apiRequest('/mtf-agent/memory', { method: 'DELETE' });
  },
};

// 关注列表API
export const watchlistAPI = {
  getWatchlist: async () => {
    const response = await apiRequest<{ watchlist?: any[]; count?: number }>('/watchlist', { method: 'GET' });
    const watchlist = (response.watchlist || [])
      .map(normalizeWatchlistItem)
      .filter((item): item is WatchlistItem => item !== null);

    return {
      ...response,
      count: typeof response.count === 'number' ? response.count : watchlist.length,
      watchlist,
    };
  },
  addToWatchlist: async (data: AddToWatchlistRequest) => {
    return apiRequest<{ message: string; id: number }>('/watchlist', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },
  removeFromWatchlist: async (id: number) => {
    return apiRequest<{ message: string }>(`/watchlist/${id}`, { method: 'DELETE' });
  },
  bindStrategy: async (symbol: string, strategyUniqueKey: string) => {
    return apiRequest<{ message: string }>('/watchlist/bind', {
      method: 'POST',
      body: JSON.stringify({ symbol, strategy_unique_key: strategyUniqueKey }),
    });
  },
};

export const financeNewsAPI = {
  list: async (params: {
    category?: FinanceNewsCategory;
    symbol?: string;
    keyword?: string;
    limit?: number;
    page?: number;
  } = {}): Promise<FinanceNewsResponse> => {
    const query = new URLSearchParams();
    if (params.category) query.set('category', params.category);
    if (params.symbol) query.set('symbol', params.symbol);
    if (params.keyword) query.set('keyword', params.keyword);
    if (params.limit) query.set('limit', String(params.limit));
    if (params.page) query.set('page', String(params.page));
    const suffix = query.toString();
    return apiRequest(`/finance-news${suffix ? `?${suffix}` : ''}`, { method: 'GET' });
  },
  hotETF: async (): Promise<HotETFResponse> => {
    return apiRequest('/finance-news/hot-etf', { method: 'GET' });
  },
};

export const getPublicPredictions = async (
  horizonLen?: number,
  symbol?: string,
): Promise<PublicPredictionResponse> => {
  const params = new URLSearchParams();
  if (horizonLen) {
    params.set('horizon_len', String(horizonLen));
  }
  if (symbol && symbol.trim()) {
    params.set('symbol', symbol.trim());
  }
  const query = params.toString();
  const url = query ? `/get-predictions/mtf-best/public?${query}` : '/get-predictions/mtf-best/public';
  return apiRequest<PublicPredictionResponse>(url, { method: 'GET' });
};

export const getAccessiblePredictions = async (
  horizonLen?: number,
  symbol?: string,
): Promise<PublicPredictionResponse> => {
  const params = new URLSearchParams();
  if (horizonLen) {
    params.set('horizon_len', String(horizonLen));
  }
  if (symbol && symbol.trim()) {
    params.set('symbol', symbol.trim());
  }
  const query = params.toString();
  const url = query ? `/get-predictions/mtf-best/accessible?${query}` : '/get-predictions/mtf-best/accessible';
  return apiRequest<PublicPredictionResponse>(url, { method: 'GET' });
};

export interface MTFBestKeysByConfig {
  symbol: string;
  mtf_version: string;
  horizon_len: number;
  context_len: number;
  mtf_lite_unique_key: string;
  mtf_pro_unique_key: string;
  non_cov_unique_key?: string;
  cov_unique_key?: string;
}

export const getMTFBestKeysByConfig = async (
  symbol: string,
  horizonLen: number,
  contextLen: number,
): Promise<{ prediction: MTFBestKeysByConfig }> => {
  const params = new URLSearchParams({
    symbol,
    horizon_len: String(horizonLen),
    context_len: String(contextLen),
  });
  return apiRequest<{ prediction: MTFBestKeysByConfig }>(
    `/save-predictions/mtf-best/by-config?${params.toString()}`,
    { method: 'GET' },
  );
};

export interface FuturePredictionsResponse {
  unique_key: string;
  dates: string[];
  predictions: number[];
  count: number;
  predicted_latest?: number;
  actual_latest?: number;
  predicted_change_percent?: number;
}

export const getFuturePredictions = async (uniqueKey: string): Promise<FuturePredictionsResponse> => {
  const params = new URLSearchParams({ unique_key: uniqueKey });
  return apiRequest<FuturePredictionsResponse>(`/get-predictions/mtf-best/future?${params.toString()}`, { method: 'GET' });
};

export interface MTFQueueBackendStatus {
  name: string;
  url: string;
  capacity: number;
  in_flight: number;
  available: number;
}

export interface MTFQueueStatus {
  queue_depth: number;
  backends?: MTFQueueBackendStatus[];
  jobs?: Record<string, number>;
}

export interface AdminGatewayBackendStatus extends TimesfmQueueBackendStatus {
  role?: string;
  supports_cov?: boolean;
  supports_direct_cov?: boolean;
  supports_non_cov?: boolean;
  supports_uzi?: boolean;
}

export interface AdminGatewayQueueStatus {
  reachable: boolean;
  status: string;
  timestamp?: string;
  source_url?: string;
  queue_depth: number;
  jobs?: Record<string, number>;
  backends?: AdminGatewayBackendStatus[];
  error?: string;
  checked_path: string;
}

export interface DirectPredictionResult {
  unique_key: string;
  stock_code: string;
  stock_type: number;
  mtf_version: string;
  context_len: number;
  horizon_len: number;
  request_end_date?: string | null;
  latest_data_date: string;
  latest_close?: number;
  history_rows?: number;
  future_dates: string[];
  best_prediction_item?: string | null;
  best_prediction_values?: number[] | null;
  predictions: Record<string, number[]>;
  cache_hit?: boolean;
}

export interface MTFPredictOnceRequest {
  stock_code: string;
  stock_type: string | number;
  prediction_type?: 'mtf-lite' | 'mtf-pro';
  time_step?: number;
  years?: number;
  horizon_len?: number;
  context_len?: number;
  user_id?: number;
  force_enqueue?: boolean;
  covariate_preset?: string;
  covariate_signature?: string;
  covariate_config?: Record<string, unknown> | boolean | null;
  covariates?: Record<string, unknown> | boolean | null;
}

export interface MTFPredictBestRequest {
  stock_code: string;
  stock_type: string | number;
  prediction_type?: 'mtf-lite' | 'mtf-pro';
  years?: number;
  horizon_len: number;
  context_len: number;
  force_enqueue?: boolean;
  covariate_preset?: string;
  covariate_signature?: string;
  covariate_config?: Record<string, unknown> | boolean | null;
  covariates?: Record<string, unknown> | boolean | null;
}

export interface MTFPredictAcceptedResponse {
  success: boolean;
  message: string;
  reused?: boolean;
  job_id: string;
  status: string;
  stock_code: string;
  force_enqueue?: boolean;
  prediction_type?: 'mtf-lite' | 'mtf-pro' | string;
  covariate_signature?: string;
  current_stage?: string;
  request_key?: string;
  created_at?: string;
  status_url?: string;
  target_path?: string;
  queue_status?: MTFQueueStatus;
  estimated_inference_time_sec?: number;
  estimated_inference_time_source?: string;
}

export type MTFJobResultPayload = {
  success?: boolean;
  stock_code?: string;
  gpu_id?: string;
  message?: string;
  error?: string;
  data?: DirectPredictionResult | Record<string, unknown>;
  result?: DirectPredictionResult | Record<string, unknown>;
  payload?: DirectPredictionResult | Record<string, unknown>;
  response?: DirectPredictionResult | Record<string, unknown>;
} & Record<string, unknown>;

export interface MTFJobStatusResponse {
  job_id: string;
  status: string;
  force_enqueue?: boolean;
  stock_code?: string;
  prediction_type?: 'mtf-lite' | 'mtf-pro' | string;
  covariate_signature?: string;
  current_stage?: string;
  target_path?: string;
  request_key?: string;
  backend?: string;
  upstream_status?: number;
  error?: string;
  created_at?: string;
  started_at?: string;
  finished_at?: string;
  result?: MTFJobResultPayload | DirectPredictionResult;
}

export interface MTFPredictOnceCachedResponse {
  success: boolean;
  stock_code?: string;
  gpu_id?: string;
  message?: string;
  error?: string;
  data?: DirectPredictionResult;
}

export const mtfAPI = {
  predictBest: async (params: MTFPredictBestRequest): Promise<MTFPredictAcceptedResponse> => {
    return apiRequest('/mtf/predict-best', {
      method: 'POST',
      body: JSON.stringify(params),
    });
  },
  predictOnce: async (params: MTFPredictOnceRequest): Promise<MTFPredictAcceptedResponse> => {
    return apiRequest('/mtf/predict-once', {
      method: 'POST',
      body: JSON.stringify(params),
    });
  },
  getPredictOnceCached: async (params: MTFPredictOnceRequest): Promise<MTFPredictOnceCachedResponse | null> => {
    const url = buildAPIURL('/mtf/predict-once/cached');
    const token = getAuthToken();
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }

    const response = await fetch(url, {
      method: 'POST',
      headers,
      body: JSON.stringify(params),
    });

    if (response.status === 404) {
      return null;
    }
    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}));
      throw new Error(errorData.error || `HTTP error! status: ${response.status}`);
    }
    return response.json();
  },
  getJobStatus: async (jobId: string): Promise<MTFJobStatusResponse> => {
    return apiRequest(`/mtf/jobs/${jobId}`, { method: 'GET' });
  },
};

export const getMarketStatus = async () => {
  // Mock data for market status
  return Promise.resolve({
    indices: [
      { name: 'S&P 500', value: 4783.45, change: 1.2 },
      { name: 'NASDAQ', value: 15055.65, change: 1.5 },
      { name: 'DOW', value: 37695.73, change: 0.8 },
      { name: 'VIX', value: 12.45, change: -5.2 }
    ]
  });
};

export const strategyAPI = {
  saveParams: async (params: any): Promise<{ message: string; unique_key: string }> => {
    return apiRequest('/strategy/params', {
      method: 'POST',
      body: JSON.stringify(params),
    });
  },
  getParams: async (uniqueKey: string): Promise<any> => {
    return apiRequest(`/strategy/params/by-unique?unique_key=${uniqueKey}`);
  },
  getUserStrategies: async (): Promise<{ strategies: any[] }> => {
    return apiRequest('/strategy/list');
  },
};

export const adminAPI = {
  getStatus: async (): Promise<{ ok: boolean; user: User }> => {
    return apiRequest('/admin/status');
  },
  getGatewayQueue: async (): Promise<AdminGatewayQueueStatus> => {
    return apiRequest('/admin/gateway-queue');
  },
  listInviteCodes: async (): Promise<{ items: MembershipInviteCode[]; count: number }> => {
    return apiRequest('/admin/invite-codes');
  },
  createInviteCode: async (payload: CreateMembershipInviteRequest): Promise<MembershipInviteCode> => {
    return apiRequest('/admin/invite-codes', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  },
  setInviteCodeActive: async (id: number, isActive: boolean): Promise<MembershipInviteCode> => {
    return apiRequest(`/admin/invite-codes/${id}/active`, {
      method: 'PATCH',
      body: JSON.stringify({ is_active: isActive }),
    });
  },
  listSystemStrategies: async (): Promise<{ strategies: StrategyParams[]; count: number }> => {
    return apiRequest('/admin/system-strategies');
  },
  saveSystemStrategy: async (payload: StrategyParams): Promise<StrategyParams> => {
    return apiRequest('/admin/system-strategies', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  },
};


export const backtestAPI = {
  runBacktest: async (params: any): Promise<any> => {
    return apiRequest('/mtf/backtest', {
      method: 'POST',
      body: JSON.stringify(params),
    });
  },
  getByUniqueKey: async (uniqueKey: string): Promise<any> => {
    const params = new URLSearchParams({ unique_key: uniqueKey });
    return apiRequest(`/mtf/backtest/by-unique?${params.toString()}`, { method: 'GET' });
  }
};

export const quotesAPI = {
  batchLatest: async (symbols: string[]): Promise<{ quotes: Array<{ symbol: string; latest_price?: number; change_percent?: number; trading_date?: string; turnover_rate?: number }> }> => {
    return apiRequest('/quotes/batch-latest', {
      method: 'POST',
      body: JSON.stringify({ symbols }),
    });
  },
};

export const stocksAPI = {
  lookupName: async (symbol: string, stockType: number): Promise<{ symbol: string; name: string }> => {
    const params = new URLSearchParams({ symbol, stock_type: String(stockType) });
    return apiRequest(`/stocks/lookup?${params.toString()}`, { method: 'GET' });
  },
};

export const normalizeUZITicker = (symbol: string): string => {
  const normalized = (symbol || '').trim().toUpperCase();
  if (normalized.startsWith('SH') && normalized.length > 2) {
    return `${normalized.slice(2)}.SH`;
  }
  if (normalized.startsWith('SZ') && normalized.length > 2) {
    return `${normalized.slice(2)}.SZ`;
  }
  return normalized;
};

export const uziAPI = {
  health: async (): Promise<UZIHealthResponse> => {
    return apiRequest('/uzi/health', { method: 'GET' });
  },
  listReports: async (ticker?: string): Promise<UZIReportListResponse> => {
    const params = new URLSearchParams();
    if (ticker) {
      params.set('ticker', ticker);
    }
    const query = params.toString();
    return apiRequest(`/uzi/reports-index${query ? `?${query}` : ''}`, { method: 'GET' });
  },
  analyzeStream: async (payload: UZIAnalyzeRequest, handlers: UZIAnalyzeStreamHandlers = {}): Promise<UZIAnalyzeResponse> => {
    const accepted = await uziAPI.enqueueAnalyze(payload, handlers.signal);
    handlers.onStart?.({
      event: 'start',
      data: {
        status: accepted.status,
        ticker: accepted.ticker || payload.ticker,
        summary: accepted.status === 'queued' ? '排队中' : '生成中',
        started_at: accepted.created_at,
      },
    });
    while (true) {
      if (handlers.signal?.aborted) {
        throw new DOMException('Aborted', 'AbortError');
      }
      await new Promise(resolve => window.setTimeout(resolve, 2000));
      const job = await uziAPI.getAnalyzeJobStatus(accepted.job_id);
      if (job.status === 'queued' || job.status === 'pending') {
        handlers.onStage?.({
          event: 'stage',
          data: {
            status: job.status,
            ticker: job.ticker || payload.ticker,
            stage: 'queued',
            summary: '排队中',
          },
        });
        continue;
      }
      if (job.status === 'running') {
        handlers.onStage?.({
          event: 'stage',
          data: {
            status: job.status,
            ticker: job.ticker || payload.ticker,
            stage: job.current_stage || 'analysis',
            summary: '生成中',
          },
        });
        continue;
      }
      if (job.status === 'succeeded') {
        const result = job.result || ({
          status: 'succeeded',
          ticker: job.ticker || payload.ticker,
          exit_code: 0,
          duration_seconds: 0,
          report: job.report,
          report_relative_path: job.report?.report_relative_path,
        } as UZIAnalyzeResponse);
        if (job.report) {
          result.report = job.report;
          result.report_relative_path = job.report.report_relative_path;
        }
        handlers.onComplete?.({ event: 'complete', data: result });
        return result;
      }
      const errorPayload: UZIAnalyzeErrorPayload = {
        status: job.status,
        ticker: job.ticker || payload.ticker,
        error: job.error || '研报生成失败',
      };
      handlers.onError?.({ event: 'error', data: errorPayload });
      throw new Error(errorPayload.error);
    }
  },
  analyze: async (payload: UZIAnalyzeRequest): Promise<UZIAnalyzeResponse> => {
    return uziAPI.analyzeStream(payload);
  },
  enqueueAnalyze: async (payload: UZIAnalyzeRequest, signal?: AbortSignal): Promise<UZIAnalyzeQueueResponse> => {
    return apiRequest('/uzi/analyze', {
      method: 'POST',
      body: JSON.stringify(payload),
      signal,
    });
  },
  getAnalyzeJobStatus: async (jobID: string): Promise<UZIAnalyzeJobStatusResponse> => {
    return apiRequest(`/uzi/jobs/${encodeURIComponent(jobID)}`, { method: 'GET' });
  },
  getAnalyzeStatus: async (): Promise<UZIAnalyzeStatus> => {
    return apiRequest('/uzi/status', { method: 'GET' });
  },
  connectAnalyzeStatus: (onMessage: (status: UZIAnalyzeStatus) => void, onError?: (error: Event) => void): WebSocket | null => {
    const token = getAuthToken();
    if (!token) {
      return null;
    }
    const url = buildWebSocketURL('/uzi/status/ws');
    url.searchParams.set('token', token);
    const socket = new WebSocket(url.toString());
    socket.onmessage = event => {
      try {
        onMessage(JSON.parse(event.data) as UZIAnalyzeStatus);
      } catch {
        // Ignore malformed keepalive or proxy messages.
      }
    };
    if (onError) {
      socket.onerror = onError;
    }
    return socket;
  },
  deleteReport: async (relativePath: string): Promise<{ success: boolean; deleted_path: string; deleted_directory: string }> => {
    const params = new URLSearchParams({ relative_path: relativePath });
    return apiRequest(`/uzi/reports-entry?${params.toString()}`, {
      method: 'DELETE',
    });
  },
  createOpenToken: async (relativePath: string): Promise<UZIReportOpenTokenResponse> => {
    return apiRequest('/uzi/reports-open-token', {
      method: 'POST',
      body: JSON.stringify({ relative_path: relativePath }),
    });
  },
  fetchReportBlob: async (relativePath: string): Promise<Blob> => {
    const response = await apiFetch(`/uzi/reports/${relativePath}`, { method: 'GET' });
    if (!response.ok) {
      let message = `HTTP error! status: ${response.status}`;
      try {
        const errorData = await response.json();
        message = errorData.error || message;
      } catch {
        // ignore json decode failure
      }
      throw new Error(message);
    }
    return response.blob();
  },
  openReport: async (relativePath: string): Promise<void> => {
    const reportWindow = window.open('', '_blank');
    if (!reportWindow) {
      throw new Error('Browser blocked opening a new tab');
    }

    reportWindow.opener = null;
    reportWindow.document.title = '正在打开研报...';

    try {
      const { open_url: openURL } = await uziAPI.createOpenToken(relativePath);
      reportWindow.location.replace(resolveOpenURL(openURL));
    } catch (error) {
      reportWindow.close();
      throw error;
    }
  },
};
