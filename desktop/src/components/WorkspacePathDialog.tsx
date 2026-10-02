import { useEffect, useRef, useState } from "react";
import { useStore } from "../store";

export function WorkspacePathDialog() {
  const request = useStore((s) => s.pathPicker);
  return request ? <PathDialog profileId={request.profileId} /> : null;
}

function PathDialog({ profileId }: { profileId: string }) {
  const profile = useStore((s) => s.profiles.find((p) => p.id === profileId));
  const dialog = useRef<HTMLDialogElement>(null);
  const [path, setPath] = useState("");
  const valid =
    path.trim().startsWith("/") || /^[a-z]:[\\/]/i.test(path.trim());
  const finish = (value: string | null) =>
    useStore.getState().resolvePathPicker(value);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    dialog.current?.showModal();
    return () => previous?.focus();
  }, []);
  return (
    <dialog
      ref={dialog}
      className="workspace-path-dialog"
      aria-labelledby="workspace-path-title"
      onCancel={(event) => {
        event.preventDefault();
        finish(null);
      }}
    >
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (valid) finish(path.trim());
        }}
      >
        <h2 id="workspace-path-title">选择远程项目</h2>
        <p>
          填写 {profile?.name || "服务主机"}{" "}
          上的工作区绝对路径。文件和工具会在该主机上运行。
        </p>
        <label>
          工作区路径
          <input
            autoFocus
            value={path}
            onChange={(event) => setPath(event.target.value)}
            placeholder="/home/q/projects/my-project"
            autoComplete="off"
            spellCheck={false}
          />
        </label>
        {path.trim() && !valid && (
          <p className="inline-error">请填写绝对路径</p>
        )}
        <div>
          <button type="button" className="btn" onClick={() => finish(null)}>
            取消
          </button>
          <button className="btn primary" disabled={!valid}>
            选择项目
          </button>
        </div>
      </form>
    </dialog>
  );
}
