#!/usr/bin/env node

import { io } from "socket.io-client";
import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";

const DEFAULT_URL = "http://115.33.108.104:31056/acp";
const DEFAULT_SOCKET_PATH = "/socket.io";
const DEFAULT_AGENT_ID = "codex";
const DEFAULT_TIMEOUT_MS = 120000;

const METHODS = {
  initialize: "initialize",
  authenticate: "authenticate",
  newSession: "session/new",
  prompt: "session/prompt",
  cancel: "session/cancel",
  load: "session/load",
  list: "session/list",
  setMode: "session/set_mode",
  setConfig: "session/set_config_option"
};

const CLIENT_METHODS = new Set([
  "session/update",
  "session/request_permission",
  "fs/read_text_file",
  "fs/write_text_file",
  "terminal/create",
  "terminal/output",
  "terminal/wait_for_exit",
  "terminal/release",
  "terminal/kill"
]);

function usage() {
  return `Usage:
  npm install
  node scripts/acp-gateway-client.mjs <command> [options]

Commands:
  init                         call initialize
  new                          call session/new
  prompt                       call session/prompt for an existing session
  chat                         initialize + session/new + session/prompt
  list                         call session/list
  load                         call session/load
  cancel                       send session/cancel notification
  raw                          call any JSON-RPC method
  repl                         interactive prompt loop in one session

Common options:
  --url <url>                  default: ${DEFAULT_URL}
  --path <path>                default: ${DEFAULT_SOCKET_PATH}
  --timeout <ms>               default: ${DEFAULT_TIMEOUT_MS}
  --json                       print JSON only
  --verbose                    print raw JSON-RPC frames

Session options:
  --agent-id <id>              _meta.agentId, default: ${DEFAULT_AGENT_ID}
  --agent-group-id <id>        _meta.agentGroupId
  --meta-json '<json>'         merged into _meta
  --mcp-json '<json-array>'    mcpServers payload, default: []

Prompt options:
  --session-id <id>            required for prompt/load/cancel
  --message <text>             prompt text
  --message-file <path>        read prompt text from file

Raw options:
  --method <name>
  --params-json '<json>'
  --notify                     send notification instead of request

Examples:
  node scripts/acp-gateway-client.mjs init
  node scripts/acp-gateway-client.mjs new --agent-id codex
  node scripts/acp-gateway-client.mjs chat --agent-id codex --message "回 ok"
  node scripts/acp-gateway-client.mjs prompt --session-id <id> --message "继续"
  node scripts/acp-gateway-client.mjs raw --method session/set_mode --params-json '{"sessionId":"<id>","modeId":"default"}'
`;
}

function parseArgs(argv) {
  const parsed = { _: [] };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (!arg.startsWith("--")) {
      parsed._.push(arg);
      continue;
    }

    const eq = arg.indexOf("=");
    const key = camelCase(arg.slice(2, eq === -1 ? undefined : eq));
    if (eq !== -1) {
      parsed[key] = arg.slice(eq + 1);
      continue;
    }

    const next = argv[i + 1];
    if (!next || next.startsWith("--")) {
      parsed[key] = true;
      continue;
    }

    parsed[key] = next;
    i += 1;
  }
  return parsed;
}

function camelCase(value) {
  return value.replace(/-([a-z])/g, (_, ch) => ch.toUpperCase());
}

class JsonRpcError extends Error {
  constructor(error) {
    super(error?.message || "JSON-RPC error");
    this.name = "JsonRpcError";
    this.code = error?.code;
    this.data = error?.data;
  }
}

class GatewayClient {
  constructor(options) {
    this.url = options.url || DEFAULT_URL;
    this.path = options.path || DEFAULT_SOCKET_PATH;
    this.timeout = Number(options.timeout || DEFAULT_TIMEOUT_MS);
    this.verbose = Boolean(options.verbose);
    this.jsonOnly = Boolean(options.json);
    this.nextID = 1;
    this.pending = new Map();
    this.terminals = new Map();

    this.socket = io(this.url, {
      path: this.path,
      transports: ["websocket"],
      reconnection: false,
      timeout: this.timeout,
      autoConnect: false
    });
  }

  async connect() {
    if (!this.jsonOnly) {
      console.error(`connect ${this.url} path=${this.path}`);
    }

    this.socket.on("acp:message", (payload) => this.onMessage(payload));
    this.socket.on("disconnect", (reason) => {
      for (const { reject, timer } of this.pending.values()) {
        clearTimeout(timer);
        reject(new Error(`socket disconnected: ${reason}`));
      }
      this.pending.clear();
    });

    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.socket.close();
        reject(new Error(`connect timeout after ${this.timeout}ms`));
      }, this.timeout);

      this.socket.once("connect", () => {
        clearTimeout(timer);
        resolve();
      });
      this.socket.once("connect_error", (err) => {
        clearTimeout(timer);
        reject(err);
      });
      this.socket.connect();
    });
  }

  close() {
    this.socket.close();
  }

  request(method, params = {}) {
    const id = this.nextID;
    this.nextID += 1;

    const message = {
      jsonrpc: "2.0",
      id,
      method,
      params
    };

    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`${method} timeout after ${this.timeout}ms`));
      }, this.timeout);

      this.pending.set(id, { method, resolve, reject, timer });
      this.emit(message);
    });
  }

  notify(method, params = {}) {
    this.emit({
      jsonrpc: "2.0",
      method,
      params
    });
  }

  emit(message) {
    if (this.verbose) {
      console.error("->", JSON.stringify(message));
    }
    this.socket.emit("acp:message", message);
  }

  onMessage(payload) {
    const message = parsePayload(payload);
    if (this.verbose) {
      console.error("<-", JSON.stringify(message));
    }

    if (message.id !== undefined && !message.method) {
      this.onResponse(message);
      return;
    }

    if (message.method) {
      this.onServerMethod(message).catch((err) => {
        if (message.id !== undefined) {
          this.emit({
            jsonrpc: "2.0",
            id: message.id,
            error: {
              code: -32603,
              message: err.message
            }
          });
        } else {
          console.error(`client handler failed for ${message.method}: ${err.message}`);
        }
      });
    }
  }

  onResponse(message) {
    const pending = this.pending.get(message.id);
    if (!pending) {
      return;
    }

    clearTimeout(pending.timer);
    this.pending.delete(message.id);

    if (message.error) {
      pending.reject(new JsonRpcError(message.error));
      return;
    }
    pending.resolve(message.result ?? null);
  }

  async onServerMethod(message) {
    if (!CLIENT_METHODS.has(message.method)) {
      console.error(`unsupported server method: ${message.method}`);
      if (message.id !== undefined) {
        this.emit({
          jsonrpc: "2.0",
          id: message.id,
          error: {
            code: -32601,
            message: `Method not found: ${message.method}`
          }
        });
      }
      return;
    }

    let result = {};
    switch (message.method) {
      case "session/update":
        printSessionUpdate(message.params);
        return;
      case "session/request_permission":
        result = handlePermission(message.params);
        break;
      case "fs/read_text_file":
        result = await readTextFile(message.params);
        break;
      case "fs/write_text_file":
        result = await writeTextFile(message.params);
        break;
      case "terminal/create":
        result = this.createTerminal(message.params);
        break;
      case "terminal/output":
        result = this.terminalOutput(message.params);
        break;
      case "terminal/wait_for_exit":
        result = this.terminalWaitForExit(message.params);
        break;
      case "terminal/release":
      case "terminal/kill":
        result = this.terminalRelease(message.params);
        break;
      default:
        result = {};
    }

    if (message.id !== undefined) {
      this.emit({
        jsonrpc: "2.0",
        id: message.id,
        result
      });
    }
  }

  createTerminal(params) {
    const terminalId = `stub-${Date.now()}-${this.terminals.size + 1}`;
    this.terminals.set(terminalId, {
      command: params?.command,
      output: `[stub terminal] command not executed by test client: ${params?.command || ""}\n`,
      exitCode: 0
    });
    return { terminalId };
  }

  terminalOutput(params) {
    const terminal = this.terminals.get(params?.terminalId);
    return {
      output: terminal?.output || "",
      truncated: false,
      exitStatus: terminal ? { exitCode: terminal.exitCode } : undefined
    };
  }

  terminalWaitForExit(params) {
    const terminal = this.terminals.get(params?.terminalId);
    return { exitCode: terminal?.exitCode ?? 0 };
  }

  terminalRelease(params) {
    if (params?.terminalId) {
      this.terminals.delete(params.terminalId);
    }
    return {};
  }
}

function parsePayload(payload) {
  if (typeof payload === "string") {
    return JSON.parse(payload);
  }
  if (Buffer.isBuffer(payload)) {
    return JSON.parse(payload.toString("utf8"));
  }
  return payload;
}

function printSessionUpdate(params) {
  const update = params?.update || {};
  const kind = update.sessionUpdate || update.type || "update";
  const text = extractText(update);
  if (text) {
    process.stderr.write(text);
    return;
  }
  console.error(`[session/update:${kind}] ${JSON.stringify(params)}`);
}

function extractText(value) {
  if (Array.isArray(value)) {
    return value.map(extractText).join("");
  }
  if (!value || typeof value !== "object") {
    return "";
  }
  if (typeof value.text === "string") {
    return value.text;
  }
  if (Array.isArray(value.content)) {
    return value.content.map(extractText).join("");
  }
  if (value.content) {
    return extractText(value.content);
  }
  return "";
}

function handlePermission(params) {
  const options = Array.isArray(params?.options) ? params.options : [];
  const allowed =
    options.find((option) => option.kind === "allow_once") ||
    options.find((option) => option.kind === "allow_always") ||
    options[0];

  if (!allowed?.optionId) {
    return { outcome: { outcome: "cancelled" } };
  }

  console.error(`[permission] auto select ${allowed.optionId}: ${allowed.name || allowed.kind}`);
  return {
    outcome: {
      outcome: "selected",
      optionId: allowed.optionId
    }
  };
}

async function readTextFile(params) {
  const target = safePath(params?.path);
  const content = await fs.readFile(target, "utf8");
  return { content };
}

async function writeTextFile(params) {
  const target = safePath(params?.path);
  await fs.mkdir(path.dirname(target), { recursive: true });
  await fs.writeFile(target, String(params?.content ?? ""), "utf8");
  return {};
}

function safePath(filePath) {
  if (!filePath || typeof filePath !== "string") {
    throw new Error("path is required");
  }
  return path.resolve(process.cwd(), filePath);
}

function clientCapabilities() {
  return {
    fs: {
      readTextFile: true,
      writeTextFile: true
    },
    terminal: true
  };
}

function initializeParams() {
  return {
    protocolVersion: 1,
    clientCapabilities: clientCapabilities()
  };
}

async function sessionParams(options) {
  const meta = parseJSONOption(options.metaJson, {});
  if (options.agentId && options.agentGroupId) {
    throw new Error("--agent-id and --agent-group-id are mutually exclusive");
  }
  if (options.agentGroupId) {
    meta.agentGroupId = options.agentGroupId;
  } else if (!meta.agentId && !meta.agentGroupId) {
    meta.agentId = options.agentId || DEFAULT_AGENT_ID;
  }

  const params = {
    mcpServers: parseJSONOption(options.mcpJson, [])
  };
  if (Object.keys(meta).length > 0) {
    params._meta = meta;
  }
  return params;
}

async function promptParams(options) {
  const sessionID = required(options.sessionId, "--session-id");
  const text = await readPromptText(options);
  return {
    sessionId: sessionID,
    prompt: [
      {
        type: "text",
        text
      }
    ]
  };
}

async function readPromptText(options) {
  if (options.messageFile) {
    return fs.readFile(options.messageFile, "utf8");
  }
  return String(options.message || "回 ok");
}

function parseJSONOption(value, fallback) {
  if (value === undefined || value === true || value === "") {
    return fallback;
  }
  return JSON.parse(value);
}

function required(value, name) {
  if (!value || value === true) {
    throw new Error(`${name} is required`);
  }
  return value;
}

function printResult(label, value, jsonOnly) {
  if (jsonOnly) {
    console.log(JSON.stringify(value, null, 2));
    return;
  }
  console.log(`\n${label}:`);
  console.log(JSON.stringify(value, null, 2));
}

async function withClient(options, fn) {
  const client = new GatewayClient(options);
  await client.connect();
  try {
    return await fn(client);
  } finally {
    client.close();
  }
}

async function runCommand(command, options) {
  switch (command) {
    case "init":
      return withClient(options, async (client) => {
        const result = await client.request(METHODS.initialize, initializeParams());
        printResult("initialize", result, options.json);
      });

    case "new":
      return withClient(options, async (client) => {
        await client.request(METHODS.initialize, initializeParams());
        const result = await client.request(METHODS.newSession, await sessionParams(options));
        printResult("session/new", result, options.json);
      });

    case "prompt":
      return withClient(options, async (client) => {
        await client.request(METHODS.initialize, initializeParams());
        const result = await client.request(METHODS.prompt, await promptParams(options));
        printResult("session/prompt", result, options.json);
      });

    case "chat":
      return withClient(options, async (client) => {
        const init = await client.request(METHODS.initialize, initializeParams());
        if (!options.json) {
          printResult("initialize", init, false);
        }

        const session = await client.request(METHODS.newSession, await sessionParams(options));
        if (!options.json) {
          printResult("session/new", session, false);
        }

        const result = await client.request(METHODS.prompt, {
          sessionId: session.sessionId,
          prompt: [
            {
              type: "text",
              text: await readPromptText(options)
            }
          ]
        });
        printResult("session/prompt", { sessionId: session.sessionId, ...result }, options.json);
      });

    case "list":
      return withClient(options, async (client) => {
        await client.request(METHODS.initialize, initializeParams());
        const params = {};
        if (options.cwd) {
          params.cwd = options.cwd;
        }
        if (options.cursor) {
          params.cursor = options.cursor;
        }
        const result = await client.request(METHODS.list, params);
        printResult("session/list", result, options.json);
      });

    case "load":
      return withClient(options, async (client) => {
        await client.request(METHODS.initialize, initializeParams());
        const result = await client.request(METHODS.load, {
          sessionId: required(options.sessionId, "--session-id"),
          cwd: options.cwd || "",
          mcpServers: parseJSONOption(options.mcpJson, [])
        });
        printResult("session/load", result, options.json);
      });

    case "cancel":
      return withClient(options, async (client) => {
        await client.request(METHODS.initialize, initializeParams());
        client.notify(METHODS.cancel, { sessionId: required(options.sessionId, "--session-id") });
        if (!options.json) {
          console.log("session/cancel sent");
        }
      });

    case "raw":
      return withClient(options, async (client) => {
        const method = required(options.method, "--method");
        const params = parseJSONOption(options.paramsJson, {});
        if (options.notify) {
          client.notify(method, params);
          if (!options.json) {
            console.log(`${method} notification sent`);
          }
          return;
        }
        const result = await client.request(method, params);
        printResult(method, result, options.json);
      });

    case "repl":
      return runRepl(options);

    case "help":
    case undefined:
      console.log(usage());
      return;

    default:
      throw new Error(`unknown command: ${command}\n\n${usage()}`);
  }
}

async function runRepl(options) {
  const readline = await import("node:readline/promises");
  const client = new GatewayClient(options);
  await client.connect();
  try {
    await client.request(METHODS.initialize, initializeParams());
    const session = await client.request(METHODS.newSession, await sessionParams(options));
    console.log(`sessionId=${session.sessionId}`);

    const rl = readline.createInterface({
      input: process.stdin,
      output: process.stdout
    });
    try {
      for (;;) {
        const line = await rl.question("> ");
        const text = line.trim();
        if (!text || text === "/exit" || text === "/quit") {
          break;
        }
        const result = await client.request(METHODS.prompt, {
          sessionId: session.sessionId,
          prompt: [{ type: "text", text }]
        });
        printResult("session/prompt", result, false);
      }
    } finally {
      rl.close();
    }
  } finally {
    client.close();
  }
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const command = options._[0] || "help";

  if (options.help || options.h) {
    console.log(usage());
    return;
  }

  await runCommand(command, options);
}

main().catch((err) => {
  if (err instanceof JsonRpcError) {
    console.error(`JSON-RPC error ${err.code}: ${err.message}`);
    if (err.data !== undefined) {
      console.error(JSON.stringify(err.data, null, 2));
    }
  } else {
    console.error(err.message);
  }
  process.exitCode = 1;
});
