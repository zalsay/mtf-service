const test = require('node:test');
const assert = require('node:assert/strict');

const {
  buildChatCompletionsBody,
  parseChatCompletionMessage,
} = require('./deepseek_tui_runtime_lib');

test('buildChatCompletionsBody forwards messages, tools, and tool_choice', () => {
  const body = buildChatCompletionsBody({
    model: 'deepseek-chat',
    messages: [{ role: 'user', content: '帮我估一下 688017' }],
    tools: [
      {
        type: 'function',
        function: {
          name: 'a_stock_data',
          description: 'A stock data',
          parameters: { type: 'object', properties: {} },
        },
      },
    ],
    toolChoice: 'auto',
    stream: false,
  });

  assert.equal(body.model, 'deepseek-chat');
  assert.deepEqual(body.messages, [{ role: 'user', content: '帮我估一下 688017' }]);
  assert.equal(body.tools[0].function.name, 'a_stock_data');
  assert.equal(body.tool_choice, 'auto');
  assert.equal(body.stream, false);
});

test('parseChatCompletionMessage extracts content and tool calls', () => {
  const parsed = parseChatCompletionMessage({
    choices: [
      {
        message: {
          role: 'assistant',
          content: '',
          reasoning_content: '需要调用 A 股数据工具。',
          tool_calls: [
            {
              id: 'call_1',
              type: 'function',
              function: {
                name: 'a_stock_data',
                arguments: '{"intent":"valuation","symbol":"688017"}',
              },
            },
          ],
        },
      },
    ],
  });

  assert.equal(parsed.content, '');
  assert.equal(parsed.reasoning_content, '需要调用 A 股数据工具。');
  assert.equal(parsed.tool_calls[0].id, 'call_1');
  assert.equal(parsed.tool_calls[0].function.name, 'a_stock_data');
});
