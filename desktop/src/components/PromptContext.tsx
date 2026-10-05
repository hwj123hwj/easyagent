import { useEffect, useMemo, useRef, useState } from "react";
import { apiRequest, useStore } from "../store";
import { EMPTY_INPUTS, type InputAttachment } from "../client/protocol";
import { AttachmentImage } from "./AttachmentImage";
import { Icon } from "./Icon";

export const WORKSPACE_FILE_DRAG = "application/x-easyagent-workspace-file";

export function PromptContext({
  id,
  cwd,
  text,
  onChange,
  onStart,
  onBusy,
  onError,
  input,
}: {
  id: string;
  cwd: string;
  text: string;
  onChange: (value: string) => void;
  onStart?: () => Promise<string>;
  onBusy: (value: boolean) => void;
  onError: (value: string) => void;
  input: React.RefObject<HTMLTextAreaElement | null>;
}) {
  const profile = useStore((state) => state.selectedProfile);
  const draft = useStore((state) => state.draftInputs[id] || EMPTY_INPUTS);
  const host = useStore(
    (state) =>
      state.profiles.find((item) => item.id === profile)?.name || "当前服务",
  );
  const picker = useRef<HTMLInputElement>(null);
  const [paths, setPaths] = useState<string[]>([]);
  const [selected, setSelected] = useState(0);
  const [dismissed, setDismissed] = useState("");
  const [uploading, setUploading] = useState(false);
  const mention = /(?:^|\s)@([^\s@]*)$/.exec(text);
  const query = mention?.[1].toLocaleLowerCase();
  const matches = useMemo(
    () =>
      query !== undefined
        ? paths
            .filter((path) => path.toLocaleLowerCase().includes(query))
            .slice(0, 12)
        : [],
    [paths, query],
  );
  const mentionOpen = !!mention && dismissed !== text;
  useEffect(() => {
    if (query === undefined || !cwd || id === "__new__") return;
    let alive = true;
    const params = new URLSearchParams({ path: cwd, session_id: id });
    void apiRequest<string[]>("GET", "/workspace/search-files?" + params)
      .then((value) => {
        if (alive) setPaths(value);
      })
      .catch((error) => {
        if (alive) onError(error.message);
      });
    return () => {
      alive = false;
    };
  }, [cwd, id, profile, query === undefined]);
  useEffect(() => {
    setSelected(0);
  }, [query]);
  const addReference = (
    path: string,
    workspace: string,
    sourceProfile = profile,
  ) => {
    if (sourceProfile !== profile || workspace !== cwd) {
      onError("这个文件来自另一个主机或项目，请在对应会话中引用。");
      return;
    }
    const current = useStore.getState().draftInputs[id] || EMPTY_INPUTS;
    if (
      current.files.some(
        (item) => item.path === path && item.workspace === workspace,
      )
    )
      return;
    if (current.files.length >= 12) {
      onError("最多引用 12 个文件");
      return;
    }
    useStore
      .getState()
      .setDraftInputs(id, {
        ...current,
        files: [...current.files, { path, workspace }],
      });
  };
  const chooseMention = (path: string) => {
    addReference(path, cwd);
    onChange(text.replace(/@[^\s@]*$/, ""));
    input.current?.focus();
  };
  const upload = async (files: File[]) => {
    if (uploading) return;
    setUploading(true);
    onBusy(true);
    onError("");
    const capturedProfile = profile;
    try {
      let target = id;
      if (id === "__new__") {
        if (!onStart) throw new Error("请先创建会话，再添加附件");
        target = await onStart();
        if (useStore.getState().selectedProfile === capturedProfile && text) {
          useStore.getState().setDraft(target, text);
          if (useStore.getState().drafts[id] === text)
            useStore.getState().setDraft(id, "");
        }
      }
      for (const file of files) {
        if (useStore.getState().selectedProfile !== capturedProfile) break;
        const current = useStore.getState().draftInputs[target] || EMPTY_INPUTS;
        if (current.attachments.length >= 8)
          throw new Error("最多添加 8 个附件");
        if (!file.size || file.size > 4 * 1024 * 1024)
          throw new Error(`${file.name} 超过 4 MiB 或为空`);
        const data = await new Promise<string>((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve(String(reader.result).split(",")[1]);
          reader.onerror = () => reject(new Error("无法读取附件"));
          reader.readAsDataURL(file);
        });
        if (useStore.getState().selectedProfile !== capturedProfile) break;
        const attachment = await apiRequest<InputAttachment>(
          "POST",
          `/sessions/${encodeURIComponent(target)}/attachments`,
          { name: file.name, data },
        );
        const latest =
          useStore.getState().selectedProfile === capturedProfile
            ? useStore.getState().draftInputs[target] || EMPTY_INPUTS
            : current;
        useStore
          .getState()
          .setDraftInputs(
            target,
            { ...latest, attachments: [...latest.attachments, attachment] },
            capturedProfile,
          );
      }
    } catch (error) {
      if (useStore.getState().selectedProfile === capturedProfile)
        onError(error instanceof Error ? error.message : "上传失败");
    } finally {
      setUploading(false);
      onBusy(false);
    }
  };
  useEffect(() => {
    const element = input.current?.closest(".personal-input-wrap");
    if (!element) return;
    const paste = (event: Event) => {
      const clipboard = (event as ClipboardEvent).clipboardData;
      const files = [...(clipboard?.files || [])].filter((file) =>
        file.type.startsWith("image/"),
      );
      if (files.length) {
        event.preventDefault();
        void upload(files);
      }
    };
    const drag = (event: Event) => {
      const transfer = (event as DragEvent).dataTransfer;
      if (
        transfer?.types.some(
          (type) => type === "Files" || type === WORKSPACE_FILE_DRAG,
        )
      )
        event.preventDefault();
    };
    const drop = (event: Event) => {
      const transfer = (event as DragEvent).dataTransfer;
      if (!transfer) return;
      const reference = transfer.getData(WORKSPACE_FILE_DRAG);
      if (reference) {
        event.preventDefault();
        event.stopPropagation();
        try {
          const value = JSON.parse(reference);
          if (
            typeof value.path !== "string" ||
            typeof value.workspace !== "string" ||
            typeof value.profile !== "string"
          )
            throw new Error();
          addReference(value.path, value.workspace, value.profile);
        } catch {
          onError("无法读取文件引用");
        }
      } else if (transfer.files.length) {
        event.preventDefault();
        event.stopPropagation();
        void upload([...transfer.files]);
      }
    };
    const keys = (event: Event) => {
      const key = event as KeyboardEvent;
      if (!mentionOpen || key.isComposing || key.keyCode === 229) return;
      if (key.key === "Escape") {
        key.preventDefault();
        key.stopPropagation();
        setDismissed(text);
      }
      if (
        matches.length &&
        ["ArrowUp", "ArrowDown", "Enter", "Tab"].includes(key.key)
      ) {
        key.preventDefault();
        key.stopPropagation();
        if (key.key === "Enter" || key.key === "Tab")
          chooseMention(matches[selected]);
        else
          setSelected(
            (index) =>
              (index + (key.key === "ArrowDown" ? 1 : -1) + matches.length) %
              matches.length,
          );
      }
    };
    element.addEventListener("paste", paste);
    element.addEventListener("dragover", drag);
    element.addEventListener("drop", drop);
    element.addEventListener("keydown", keys, true);
    return () => {
      element.removeEventListener("paste", paste);
      element.removeEventListener("dragover", drag);
      element.removeEventListener("drop", drop);
      element.removeEventListener("keydown", keys, true);
    };
  }, [id, cwd, profile, text, selected, paths, dismissed, uploading]);
  return (
    <>
      <input
        ref={picker}
        type="file"
        multiple
        hidden
        onChange={(event) => {
          void upload([...(event.target.files || [])]);
          event.target.value = "";
        }}
      />
      <button
        className="composer-attach icon-btn"
        aria-label="添加附件"
        title="添加附件 · 支持拖入文件或粘贴截图（每个 4 MiB）"
        disabled={uploading}
        onClick={() => picker.current?.click()}
      >
        <Icon name="plus" size={16} />
      </button>
      {(draft.attachments.length > 0 ||
        draft.files.length > 0 ||
        uploading) && (
        <div className="composer-context" aria-label="消息上下文">
          {draft.attachments.map((item) => {
            const isImage = item.mime_type?.startsWith("image/");
            return (
              <span
                key={item.id}
                className={isImage ? "is-image" : undefined}
                title={`${host} · ${item.workspace}\n${item.path}`}
              >
                {isImage ? (
                  <AttachmentImage attachmentId={item.id} sessionId={id} name={item.name} className="composer-attachment-thumb" />
                ) : (
                  <Icon name="file" size={12} />
                )}
                {item.name}
                <small>{host}</small>
                <button
                  aria-label={`移除 ${item.name}`}
                  onClick={() =>
                    useStore
                      .getState()
                      .setDraftInputs(id, {
                        ...draft,
                        attachments: draft.attachments.filter(
                          (attachment) => attachment.id !== item.id,
                        ),
                      })
                  }
                >
                  ×
                </button>
              </span>
            );
          })}
          {draft.files.map((item) => (
            <span key={item.path} title={`${host} · ${item.workspace}`}>
              <Icon name="folder" size={12} />@{item.path.split(/[\\/]/).pop()}
              <small>
                {host} · {item.workspace.split(/[\\/]/).pop()}
              </small>
              <button
                aria-label={`移除文件引用 ${item.path}`}
                onClick={() =>
                  useStore
                    .getState()
                    .setDraftInputs(id, {
                      ...draft,
                      files: draft.files.filter(
                        (file) => file.path !== item.path,
                      ),
                    })
                }
              >
                ×
              </button>
            </span>
          ))}
          {uploading && <span role="status">正在上传…</span>}
        </div>
      )}
      {mentionOpen && (
        <div
          className="file-mention-menu"
          role="listbox"
          aria-label="选择项目文件"
        >
          <header>
            {host} · {cwd.split(/[\\/]/).pop()}
            <small>↑ ↓ 选择 · Enter 引用</small>
          </header>
          {matches.map((path, index) => (
            <button
              key={path}
              role="option"
              aria-selected={selected === index}
              onPointerDown={(event) => event.preventDefault()}
              onClick={() => chooseMention(path)}
            >
              <Icon name="file" size={12} />
              {path}
            </button>
          ))}
          {!matches.length && (
            <p>
              {id === "__new__" ? "创建项目会话后可引用文件" : "没有匹配的文件"}
            </p>
          )}
        </div>
      )}

    </>
  );
}
