import { useState } from "react";
import { useStore } from "../store";
import type { Confirmation } from "../client/protocol";
import { answersComplete, parseQuestions, type QuestionAnswer } from "../client/questions";
import { useT } from "../i18n/useT";
import { Markdown } from "./Markdown";

export function QuestionPrompt({ id, value }: { id: string; value: Confirmation }) {
  const t = useT(), questions = parseQuestions(value.args);
  const [answers, setAnswers] = useState<QuestionAnswer[]>(() => questions.map(() => ({ selected: [] })));
  const [other, setOther] = useState<boolean[]>(() => questions.map(() => false));
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  function update(index: number, answer: QuestionAnswer) {
    setAnswers((current) => current.map((a, i) => i === index ? answer : a));
  }
  async function reply(approved: boolean) {
    if (busy || (approved && !answersComplete(questions, answers))) return;
    setBusy(true); setError("");
    try {
      await useStore.getState().confirm(id, value.confirmation_id, approved,
        approved ? answers.map((a) => ({ ...a, text: a.text?.trim() || undefined })) : undefined);
    } catch (err) { setError((err as Error).message); setBusy(false); }
  }
  return <section className="question-prompt" aria-label={t("ask.title")} aria-busy={busy}>
    <strong>{t("ask.title")}</strong>
    <div className="question-prompt-body">
      {!questions.length && <p role="alert">{t("ask.invalid")}</p>}
      {questions.map((q, i) => <fieldset className="ask-question" key={i} disabled={busy}>
        <legend className="ask-q-head">
          {q.header && <span className="ask-q-chip">{q.header}</span>}
          <span className="ask-q-text">{q.question}</span>
          {q.multiSelect && <span className="ask-q-hint">{t("ask.multiHint")}</span>}
        </legend>
        <div className="ask-options">
          {q.options.map((o) => <label className={"ask-option" + (answers[i].selected.includes(o.label) ? " active" : "")} key={o.label}>
            <input type={q.multiSelect ? "checkbox" : "radio"} name={value.confirmation_id + i} aria-label={o.label}
              checked={answers[i].selected.includes(o.label)} onChange={() => {
                if (!q.multiSelect) {
                  setOther((v) => v.map((x, n) => n === i ? false : x));
                  update(i, { selected: [o.label] });
                } else {
                  update(i, { ...answers[i], selected: answers[i].selected.includes(o.label)
                    ? answers[i].selected.filter((x) => x !== o.label) : [...answers[i].selected, o.label] });
                }
              }} />
            <span className="ask-option-body"><span className="ask-option-label">{o.label}</span>
              {o.description && <span className="ask-option-desc">{o.description}</span>}
            </span>
          </label>)}
          <label className={"ask-option" + (other[i] ? " active" : "")}>
            <input type={q.multiSelect ? "checkbox" : "radio"} name={value.confirmation_id + i} aria-label={t("ask.other")}
              checked={other[i]} onChange={() => {
                const enabled = !other[i];
                setOther((v) => v.map((x, n) => n === i ? enabled : x));
                update(i, { selected: q.multiSelect ? answers[i].selected : [], text: enabled ? "" : undefined });
              }} />
            <span className="ask-option-label">{t("ask.other")}</span>
          </label>
          {other[i] && <textarea className="ask-other-input" aria-label={q.question + " · " + t("ask.other")}
            placeholder={t("ask.otherPlaceholder")} value={answers[i].text || ""} maxLength={4000}
            onChange={(e) => update(i, { ...answers[i], text: e.target.value })} />}
        </div>
        {q.options.filter((o) => o.preview && answers[i].selected.includes(o.label)).map((o) =>
          <div className="ask-answer-preview" key={o.label}><Markdown text={o.preview!} /></div>)}
      </fieldset>)}
    </div>
    <div className="question-prompt-actions">
      <button className="btn" disabled={busy} onClick={() => void reply(false)}>{t("ask.skip")}</button>
      <button className="btn primary" disabled={busy || !answersComplete(questions, answers)}
        onClick={() => void reply(true)}>{busy ? t("ask.submitting") : t("ask.submit")}</button>
    </div>
    {error && <p role="alert">{error}</p>}
  </section>;
}
