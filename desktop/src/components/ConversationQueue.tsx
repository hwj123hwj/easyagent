import { useState } from "react";
import { useStore, type SessionView } from "../store";
import { Icon } from "./Icon";

export function ConversationQueue({ view }: { view: SessionView }) {
  const queue = view.queue;
  const [editing, setEditing] = useState<string>();
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  if (!queue?.items.length) return null;
  const act = async (action: string, id?: string, prompt?: string) => {
    setBusy(true);
    setError("");
    try {
      await useStore.getState().changeQueue(view.meta.id, action, id, prompt);
      setEditing(undefined);
    } catch (error) {
      setError(error instanceof Error ? error.message : "队列操作失败");
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="conversation-queue" aria-label="待发消息">
      <header>
        <strong>
          {queue.paused ? "待发队列已暂停" : "当前任务结束后执行"} ·{" "}
          {queue.items.length}
        </strong>
        <button
          disabled={busy}
          onClick={() => void act(queue.paused ? "resume" : "pause")}
        >
          {queue.paused ? "恢复队列" : "暂停"}
        </button>
      </header>
      {queue.items.map((item, index) => (
        <div className="queue-message" key={item.id}>
          <span className="queue-number">{index + 1}</span>
          {editing === item.id ? (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                void act("edit", item.id, draft);
              }}
            >
              <textarea
                autoFocus
                aria-label="编辑待发消息"
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
              />
              <button disabled={busy || !draft.trim()}>保存</button>
              <button
                type="button"
                disabled={busy}
                onClick={() => setEditing(undefined)}
              >
                取消
              </button>
            </form>
          ) : (
            <>
              <p title={item.prompt}>
                {item.prompt}
                {(item.inputs?.attachments?.length || 0) +
                  (item.inputs?.files?.length || 0) >
                  0 && (
                  <small>
                    {" "}
                    ·{" "}
                    {(item.inputs?.attachments?.length || 0) +
                      (item.inputs?.files?.length || 0)}{" "}
                    个上下文
                  </small>
                )}
              </p>
              <div className="queue-actions">
                <button
                  disabled={busy}
                  onClick={() => {
                    setEditing(item.id);
                    setDraft(item.prompt);
                  }}
                >
                  编辑
                </button>
                <button
                  disabled={busy}
                  title="停止当前任务，优先发送这一条；其他消息继续保留"
                  onClick={() => void act("send_now", item.id)}
                >
                  立即发送
                </button>
                <button
                  disabled={busy}
                  aria-label="删除待发消息"
                  onClick={() => void act("remove", item.id)}
                >
                  <Icon name="x" size={12} />
                </button>
              </div>
            </>
          )}
        </div>
      ))}
      {error && (
        <p role="alert" className="composer-error">
          {error}
        </p>
      )}
    </section>
  );
}
