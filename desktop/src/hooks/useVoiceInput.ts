import { useRef, useState, useCallback, useEffect } from "react";
import { Capacitor, registerPlugin } from "@capacitor/core";
import { getBaseUrl, authHeaders } from "../store";

/**
 * useVoiceInput — Microphone recording + ASR transcription hook.
 *
 * Two strategies depending on platform:
 *
 * - **Android (Capacitor)**: Uses a native MediaRecorder plugin
 *   (NativeRecorderPlugin.java) to bypass WebView getUserMedia limitations.
 *   The native recorder captures AAC audio → base64 → JS converts to Blob →
 *   upload to /asr/transcribe.
 *
 * - **Desktop/Browser**: Uses standard MediaRecorder + getUserMedia API.
 */

// Register the native plugin (safe to call on web — returns a no-op proxy)
interface NativeRecorderPlugin {
  hasPermission(): Promise<{ granted: boolean }>;
  askPermission(): Promise<{ granted: boolean }>;
  start(): Promise<void>;
  stop(): Promise<{ base64: string; mimeType: string; duration: number }>;
}
const NativeRecorder = registerPlugin<NativeRecorderPlugin>("NativeRecorder");

type VoiceInputOptions = {
  onText: (text: string) => void;
};

export function useVoiceInput({ onText }: VoiceInputOptions) {
  const [recording, setRecording] = useState(false);
  const [transcribing, setTranscribing] = useState(false);
  const [error, setError] = useState("");
  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const onTextRef = useRef(onText);
  onTextRef.current = onText;
  useEffect(
    () => () => {
      const recorder = mediaRecorderRef.current;
      if (recorder) {
        recorder.onstop = null;
        recorder.stream.getTracks().forEach((track) => track.stop());
        if (recorder.state !== "inactive") recorder.stop();
      }
    },
    [],
  );
  const chunksRef = useRef<Blob[]>([]);

  // ─── Native Recording (Android) ────────────────────────────────────

  const startNativeRecording = async (): Promise<boolean> => {
    try {
      console.log(
        "[VoiceInput] Requesting mic permission via native plugin...",
      );
      const perm = await NativeRecorder.askPermission();
      console.log("[VoiceInput] Permission result:", perm);
      if (!perm.granted) {
        setError("麦克风权限被拒绝");
        return false;
      }

      console.log("[VoiceInput] Starting native recording...");
      await NativeRecorder.start();
      console.log("[VoiceInput] Native recording started");
      return true;
    } catch (err) {
      console.error("[VoiceInput] Native start failed:", err);
      throw err;
    }
  };

  const stopNativeRecording = async (): Promise<void> => {
    console.log("[VoiceInput] Stopping native recording...");
    const result = await NativeRecorder.stop();
    console.log(
      "[VoiceInput] Native recording stopped, base64 length:",
      result.base64.length,
    );

    // Convert base64 to Blob
    const byteChars = atob(result.base64);
    const byteArray = new Uint8Array(byteChars.length);
    for (let i = 0; i < byteChars.length; i++) {
      byteArray[i] = byteChars.charCodeAt(i);
    }
    const blob = new Blob([byteArray], { type: result.mimeType });

    // Upload for transcription
    await uploadForTranscription(blob, "voice.m4a");
  };

  // ─── Browser Recording (getUserMedia) ──────────────────────────────

  const startBrowserRecording = async (): Promise<void> => {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    const mimeType = MediaRecorder.isTypeSupported("audio/webm;codecs=opus")
      ? "audio/webm;codecs=opus"
      : MediaRecorder.isTypeSupported("audio/webm")
        ? "audio/webm"
        : "";

    const mr = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
    chunksRef.current = [];

    mr.ondataavailable = (e) => {
      if (e.data.size > 0) chunksRef.current.push(e.data);
    };

    mr.onstop = async () => {
      stream.getTracks().forEach((t) => t.stop());
      const blob = new Blob(chunksRef.current, {
        type: mimeType || "audio/webm",
      });
      if (blob.size < 100) {
        setError("录音太短，请重试");
        return;
      }
      const ext = mimeType.includes("webm") ? "webm" : "wav";
      await uploadForTranscription(blob, `voice.${ext}`);
    };

    mr.start();
    mediaRecorderRef.current = mr;
  };

  // ─── Shared upload logic ───────────────────────────────────────────

  const uploadForTranscription = async (
    blob: Blob,
    filename: string,
  ): Promise<void> => {
    setTranscribing(true);
    try {
      if (window.piAPI) {
        const bytes = new Uint8Array(await blob.arrayBuffer());
        let binary = "";
        for (const byte of bytes) binary += String.fromCharCode(byte);
        const data = await window.piAPI.uploadAudio(
          btoa(binary),
          blob.type,
          filename,
        );
        if (data.text) onTextRef.current(data.text);
        else setError("未能识别语音内容，请重试");
        return;
      }
      const formData = new FormData();
      formData.append("file", blob, filename);

      const url = `${getBaseUrl()}/asr/transcribe`;
      console.log(
        "[VoiceInput] Uploading to:",
        url,
        "blob size:",
        blob.size,
        "type:",
        blob.type,
      );

      const res = await fetch(url, {
        method: "POST",
        headers: authHeaders(),
        body: formData,
      });

      console.log("[VoiceInput] Response status:", res.status, res.statusText);

      if (!res.ok) {
        const errText = await res.text().catch(() => "unknown");
        console.error("[VoiceInput] Server error:", res.status, errText);
        let errMsg = `Server error ${res.status}`;
        try {
          const errJson = JSON.parse(errText);
          errMsg = errJson.error || errMsg;
        } catch {
          /* not JSON */
        }
        throw new Error(errMsg);
      }

      const data = await res.json();
      console.log("[VoiceInput] ASR result:", JSON.stringify(data));

      if (data.text) {
        onTextRef.current(data.text);
      } else {
        setError("未能识别语音内容，请重试");
      }
    } catch (err) {
      console.error("[VoiceInput] Upload failed:", err);
      const msg = err instanceof Error ? err.message : "语音识别失败";
      // If it's a network error, show a helpful message
      if (msg.includes("Failed to fetch") || msg.includes("NetworkError")) {
        setError("网络连接失败，请检查服务器地址是否正确");
      } else {
        setError(msg);
      }
    } finally {
      setTranscribing(false);
    }
  };

  // ─── Unified toggle ────────────────────────────────────────────────

  const startRecording = useCallback(async () => {
    setError("");
    try {
      if (Capacitor.isNativePlatform()) {
        // Android: use native recorder
        const started = await startNativeRecording();
        if (!started) return;
      } else {
        // Desktop/browser
        await startBrowserRecording();
      }
      setRecording(true);
    } catch (err) {
      console.error("[VoiceInput] Start failed:", err);
      if (err instanceof DOMException && err.name === "NotAllowedError") {
        setError("麦克风权限被拒绝");
      } else if (err instanceof Error && err.message.includes("permission")) {
        setError("麦克风权限被拒绝");
      } else {
        setError(
          "无法访问麦克风: " +
            (err instanceof Error ? err.message : "未知错误"),
        );
      }
    }
  }, []);

  const stopRecording = useCallback(async () => {
    setRecording(false);
    try {
      if (Capacitor.isNativePlatform() && !mediaRecorderRef.current) {
        // Native recording
        await stopNativeRecording();
      } else if (mediaRecorderRef.current) {
        // Browser recording
        const mr = mediaRecorderRef.current;
        if (mr.state !== "inactive") {
          mr.stop();
        }
        mediaRecorderRef.current = null;
      }
    } catch (err) {
      console.error("[VoiceInput] Stop failed:", err);
      setError(err instanceof Error ? err.message : "录音停止失败");
    }
  }, []);

  const toggle = useCallback(() => {
    if (recording) {
      void stopRecording();
    } else {
      void startRecording();
    }
  }, [recording, startRecording, stopRecording]);

  return { recording, toggle, transcribing, error, setError };
}
