export interface Recovery {
  kind: "authentication" | "rate_limit" | "network" | "other";
  message: string;
  settings?: "connections" | "models";
  action: string;
}
export function runRecovery(error: string): Recovery {
  if (
    /认证|unauthori[sz]ed|invalid.*(?:key|token)|\b40[13]\b|authentication/i.test(
      error,
    )
  )
    return {
      kind: "authentication",
      message: "凭据未被服务接受。检查连接令牌或模型密钥后重新发送。",
      settings: /服务认证|连接令牌|bearer/i.test(error)
        ? "connections"
        : "models",
      action: "检查凭据",
    };
  if (/\b429\b|rate.?limit|限流|quota|配额/i.test(error))
    return {
      kind: "rate_limit",
      message:
        "模型服务暂时限流或配额不足。检查额度，稍后再发送；任务不会自动重试。",
      settings: "models",
      action: "检查模型服务",
    };
  if (
    /network|fetch failed|ECONN|timeout|timed? ?out|断线|连接|超时|socket/i.test(
      error,
    )
  )
    return {
      kind: "network",
      message: "连接中断或请求超时。重新连接后先核对执行记录，再决定是否重发。",
      settings: "connections",
      action: "检查连接",
    };
  return {
    kind: "other",
    message: "任务未正常完成。请展开工具结果和错误详情，核对已执行的操作。",
    action: "查看详情",
  };
}
