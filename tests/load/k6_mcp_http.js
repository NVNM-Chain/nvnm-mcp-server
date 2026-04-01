/**
 * k6 load tests for Inveniam EVM MCP server (Streamable HTTP / JSON-RPC 2.0).
 *
 * Default endpoint: http://localhost:8080/ (MCP handler root for make run-http)
 * Override: MCP_URL=http://host:port/path (e.g. .../mcp behind a proxy)
 *
 * Run one scenario: k6 run --scenario constant_reads tests/load/k6_mcp_http.js
 */

import http from 'k6/http';
import { check, sleep } from 'k6';

// `make run-http` serves MCP at the root of MCP_HTTP_ADDR (default :8080). Use a path
// suffix such as /mcp when your ingress or proxy mounts the handler there.
const DEFAULT_MCP_URL = 'http://localhost:8080/';

function mcpURL() {
  return __ENV.MCP_URL || DEFAULT_MCP_URL;
}

function rpcHeaders(sessionId) {
  const h = {
    'Content-Type': 'application/json',
    // Required by github.com/modelcontextprotocol/go-sdk StreamableHTTPHandler for POST.
    Accept: 'application/json, text/event-stream',
  };
  if (sessionId) {
    h['Mcp-Session-Id'] = sessionId;
  }
  return h;
}

/**
 * k6 normalizes response header names to lowercase.
 */
function sessionIDFromResponse(res) {
  if (!res || !res.headers) {
    return '';
  }
  return res.headers['mcp-session-id'] || '';
}

function nextRPCID() {
  return Math.floor(Math.random() * 0x7fffffff);
}

function rpcIDsMatch(sent, received) {
  return sent === received || String(sent) === String(received);
}

function parseJSONBody(res) {
  try {
    return res.json();
  } catch (e) {
    return null;
  }
}

/**
 * Collect JSON values from a Streamable HTTP body: either a single JSON document
 * or SSE `data:` lines (JSONResponse off).
 */
function jsonMessagesFromResponse(res) {
  const out = [];
  const direct = parseJSONBody(res);
  if (direct !== null) {
    out.push(direct);
    return out;
  }
  const text = String(res.body);
  if (!text.includes('data:')) {
    return out;
  }
  const lines = text.split('\n');
  for (let i = 0; i < lines.length; i++) {
    const trimmed = lines[i].trim();
    if (!trimmed.startsWith('data:')) {
      continue;
    }
    const payload = trimmed.slice(5).trim();
    if (payload === '' || payload === '[DONE]') {
      continue;
    }
    try {
      out.push(JSON.parse(payload));
    } catch (e2) {
      continue;
    }
  }
  return out;
}

function jsonRPCMessageForID(res, expectedID) {
  const msgs = jsonMessagesFromResponse(res);
  for (let i = 0; i < msgs.length; i++) {
    const m = msgs[i];
    if (m && m.jsonrpc === '2.0' && rpcIDsMatch(expectedID, m.id)) {
      return m;
    }
  }
  return null;
}

/**
 * Validates a JSON-RPC success response (initialize, etc.).
 */
function checkJSONRPCResult(res, expectedID) {
  if (res.status !== 200) {
    return false;
  }
  const body = jsonRPCMessageForID(res, expectedID);
  if (!body) {
    return false;
  }
  if (body.error) {
    return false;
  }
  return body.result !== undefined;
}

/**
 * Validates tools/call success envelope (MCP CallToolResult).
 */
function checkToolsCallResult(res, expectedID) {
  if (res.status !== 200) {
    return false;
  }
  const body = jsonRPCMessageForID(res, expectedID);
  if (!body) {
    return false;
  }
  if (body.error) {
    return false;
  }
  const result = body.result;
  if (!result || typeof result !== 'object') {
    return false;
  }
  if (!Array.isArray(result.content)) {
    return false;
  }
  if (result.isError === true) {
    return false;
  }
  return true;
}

/**
 * POST JSON-RPC to the MCP endpoint.
 * @param {string} url
 * @param {object} payload plain object (will be JSON.stringify)
 * @param {string} sessionId
 * @param {object} tags k6 metric tags
 */
function mcpPost(url, payload, sessionId, tags) {
  const body = JSON.stringify(payload);
  return http.post(url, body, {
    headers: rpcHeaders(sessionId),
    tags: tags || {},
  });
}

/**
 * MCP tools/call helper.
 * @param {{ url: string, sessionId: string }} data from setup()
 * @param {string} toolName
 * @param {object} args tool arguments
 * @param {object} [extraTags]
 */
export function mcpToolCall(data, toolName, args, extraTags) {
  const id = nextRPCID();
  const payload = {
    jsonrpc: '2.0',
    method: 'tools/call',
    id: id,
    params: {
      name: toolName,
      arguments: args || {},
    },
  };
  const tags = Object.assign(
    { mcp_method: 'tools/call', mcp_tool: toolName },
    extraTags || {},
  );
  const res = mcpPost(data.url, payload, data.sessionId, tags);
  check(res, {
    'tools/call HTTP 200': (r) => r.status === 200,
    'tools/call JSON-RPC result': () => checkToolsCallResult(res, id),
  });
  return res;
}

export const options = {
  scenarios: {
    constant_reads: {
      executor: 'constant-vus',
      vus: 10,
      duration: '2m',
      exec: 'constantReads',
      tags: { scenario: 'constant_reads' },
    },
    burst_reads: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '1m', target: 50 },
        { duration: '1m', target: 50 },
        { duration: '1m', target: 0 },
      ],
      exec: 'burstReads',
      tags: { scenario: 'burst_reads' },
    },
    mixed_workload: {
      executor: 'constant-vus',
      vus: 15,
      duration: '2m',
      exec: 'mixedWorkload',
      tags: { scenario: 'mixed_workload' },
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<2000'],
    http_req_failed: ['rate<0.01'],
  },
};

export function setup() {
  const url = mcpURL();
  const initID = 1;
  const initPayload = {
    jsonrpc: '2.0',
    method: 'initialize',
    id: initID,
    params: {
      protocolVersion: '2025-03-26',
      capabilities: {},
      clientInfo: { name: 'k6-load-test', version: '1.0.0' },
    },
  };

  const initRes = mcpPost(url, initPayload, '', {
    mcp_method: 'initialize',
  });

  const initOK = check(initRes, {
    'initialize HTTP 200': (r) => r.status === 200,
    'initialize JSON-RPC result': () => checkJSONRPCResult(initRes, initID),
  });

  if (!initOK) {
    throw new Error(
      `MCP initialize failed: status=${initRes.status} body=${String(
        initRes.body,
      ).slice(0, 500)}`,
    );
  }

  const sessionId = sessionIDFromResponse(initRes);

  const initializedPayload = {
    jsonrpc: '2.0',
    method: 'notifications/initialized',
    params: {},
  };
  const notifRes = mcpPost(url, initializedPayload, sessionId, {
    mcp_method: 'notifications/initialized',
  });
  check(notifRes, {
    'notifications/initialized HTTP accepted': (r) =>
      r.status === 200 || r.status === 202 || r.status === 204,
  });

  return { url: url, sessionId: sessionId };
}

export function constantReads(data) {
  mcpToolCall(data, 'evm_get_chain_id', {});
  sleep(0.5);
}

export function burstReads(data) {
  mcpToolCall(data, 'evm_get_chain_id', {});
  sleep(0.2);
}

export function mixedWorkload(data) {
  const r = Math.random();
  if (r < 0.34) {
    mcpToolCall(data, 'evm_get_chain_id', {});
  } else if (r < 0.67) {
    mcpToolCall(data, 'evm_get_block', {});
  } else {
    mcpToolCall(data, 'anchor_get_registries', { limit: 10 });
  }
  sleep(0.3);
}
