export interface Command {
  name: string;
  label: string;
  description: string;
  subcommands?: Array<{ name: string; description: string }>;
  insert?: string;
}
export const commands: Command[] = [
  { name: "help", label: "使用帮助", description: "查看命令与输入快捷键" },
  { name: "new", label: "新对话", description: "保留历史，开始新的对话" },
  {
    name: "model",
    label: "切换模型",
    description: "打开搜索列表，或追加模型名称",
  },
  { name: "sessions", label: "历史会话", description: "打开或收起会话侧栏" },
  {
    name: "workflow",
    label: "动态工作流",
    description: "追加任务，让 Agent 设计并执行",
  },
  { name: "tools", label: "可用工具", description: "查看当前 Agent 的工具" },
  {
    name: "context",
    label: "会话信息",
    description: "查看模型、工作区与路径权限",
  },
  {
    name: "compact",
    label: "压缩上下文",
    description: "总结较早消息，可追加保留要求",
  },
  { name: "stop", label: "停止任务", description: "停止当前会话的运行" },
  {
    name: "mcp",
    label: "MCP 工具",
    description: "管理工具连接、项目信任与授权",
  },
  {
    name: "settings",
    label: "工作区设置",
    description: "管理主机连接与界面偏好",
  },
];
export function parseCommand(text: string) {
  const match = text.trim().match(/^\/([a-z]+)(?:\s+([\s\S]*))?$/i);
  return match
    ? { name: match[1].toLowerCase(), args: (match[2] || "").trim() }
    : null;
}
export function filterCommands(
  text: string,
  catalog: Command[] = commands,
): Command[] | null {
  if (!text.startsWith("/") || text.startsWith("//") || text.includes("\n"))
    return null;
  const sub = text.match(/^\/([a-z]+)\s+([^\s]*)$/i);
  if (sub) {
    const command = catalog.find((c) => c.name === sub[1]);
    return (
      command?.subcommands
        ?.filter((c) =>
          c.name.toLocaleLowerCase().startsWith(sub[2].toLocaleLowerCase()),
        )
        .map((c) => ({
          name: command.name + " " + c.name,
          label: c.name,
          description: c.description,
          insert: "/" + command.name + " " + c.name + " ",
        })) || null
    );
  }
  if (!/^\/[^\s/]*$/.test(text)) return null;
  const query = text.slice(1).toLocaleLowerCase();
  return catalog.filter((c) =>
    (c.name + " " + c.label + " " + c.description)
      .toLocaleLowerCase()
      .includes(query),
  );
}
