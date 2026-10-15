import { useState } from "react";
import { useStore, type SessionView } from "../store";
import { runRecovery } from "../client/run-recovery";
export function RunRecovery({ view }: { view: SessionView }) {
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const error =
    view.run?.error ||
    view.transcript
      .filter((i) => i.kind === "error")
      .map((i) => (i.kind === "error" ? i.text : ""))
      .at(-1) ||
    "";
  if (view.phase !== "error") return null;
  const recovery = runRecovery(error),
    host = useStore.getState().selectedProfile;
  async function resend() {
    if (!view.run?.prompt) return;
    if (
      !window.confirm(
        "重新发送可能重复已完成的操作。请先检查工具记录，确认可以再次执行。",
      )
    )
      return;
    setBusy(true);
    try {
      if (host !== useStore.getState().selectedProfile)
        throw new Error("主机已切换");
      await useStore
        .getState()
        .sendPrompt(view.meta.id, view.run.prompt, view.run.inputs);
    } catch (e) {
      setNotice((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="run-recovery" role="status">
      <p>{recovery.message}</p>
      <div>
        {recovery.settings && (
          <button
            className="btn"
            onClick={() =>
              useStore.getState().openSettings(true, recovery.settings)
            }
          >
            {recovery.action}
          </button>
        )}
        {recovery.kind === "network" && (
          <button
            className="btn"
            disabled={busy}
            onClick={() => {
              setBusy(true);
              void useStore
                .getState()
                .connectProfile(host)
                .catch((e) => setNotice(e.message))
                .finally(() => setBusy(false));
            }}
          >
            重新连接
          </button>
        )}
        {view.run?.prompt && (
          <button className="btn" disabled={busy} onClick={() => void resend()}>
            检查后重新发送
          </button>
        )}
      </div>
      {notice && <p role="alert">{notice}</p>}
    </div>
  );
}
