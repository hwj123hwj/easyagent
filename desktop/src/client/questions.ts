export interface UserQuestion {
  question: string;
  header?: string;
  options: { label: string; description?: string; preview?: string }[];
  multiSelect?: boolean;
}
export interface QuestionAnswer { selected: string[]; text?: string }
export function parseQuestions(args: unknown): UserQuestion[] {
  if (!args || typeof args !== "object" || !("questions" in args)) return [];
  const questions = args.questions;
  if (!Array.isArray(questions) || !questions.length || questions.length > 4) return [];
  if (!questions.every((q) => q && typeof q.question === "string" && q.question.trim() &&
    (q.header === undefined || typeof q.header === "string") &&
    (q.multiSelect === undefined || typeof q.multiSelect === "boolean") &&
    Array.isArray(q.options) && q.options.length >= 2 && q.options.length <= 4 &&
    q.options.every((o: UserQuestion["options"][number]) => o && typeof o.label === "string" && o.label.trim() &&
      (o.description === undefined || typeof o.description === "string") &&
      (o.preview === undefined || typeof o.preview === "string")))) return [];
  return questions;
}
export function answersComplete(questions: UserQuestion[], answers: QuestionAnswer[]): boolean {
  return !!questions.length && questions.length === answers.length && questions.every((q, i) => {
    const a = answers[i], count = a.selected.length + (a.text?.trim() ? 1 : 0);
    return count > 0 && (q.multiSelect || count === 1) && new Set(a.selected).size === a.selected.length &&
      a.selected.every((label) => q.options.some((o) => o.label === label));
  });
}
