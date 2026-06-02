function normalizeMessages(messages, prompt) {
  if (Array.isArray(messages) && messages.length > 0) {
    return messages
      .filter((message) => message && typeof message === 'object' && message.role)
      .map((message) => {
        const normalized = { ...message, role: String(message.role) };
        if (normalized.content === undefined || normalized.content === null) {
          normalized.content = '';
        }
        return normalized;
      });
  }
  const content = String(prompt || '').trim();
  return content ? [{ role: 'user', content }] : [];
}

function buildChatCompletionsBody({ model, messages, prompt, tools, toolChoice, stream }) {
  const body = {
    model,
    messages: normalizeMessages(messages, prompt),
    stream: Boolean(stream),
  };
  if (Array.isArray(tools) && tools.length > 0) {
    body.tools = tools;
    body.tool_choice = toolChoice || 'auto';
  }
  return body;
}

function parseChatCompletionMessage(decoded) {
  const choice = decoded?.choices?.[0] || {};
  const message = choice.message || {};
  return {
    content: String(message.content || choice.text || ''),
    reasoning_content: String(message.reasoning_content || ''),
    tool_calls: Array.isArray(message.tool_calls) ? message.tool_calls : [],
    role: message.role || 'assistant',
  };
}

module.exports = {
  buildChatCompletionsBody,
  normalizeMessages,
  parseChatCompletionMessage,
};
