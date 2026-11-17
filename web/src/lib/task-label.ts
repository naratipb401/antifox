import type { Task } from "@/lib/types";

// taskLabel renders a concise, identifiable task label used by every task filter
// (findings / dashboard / assets). Prefers the user-set name/goal; falls back to
// "#id description(truncated)" so a task is always uniquely recognizable.
export function taskLabel(t: { id: string; name?: string; description?: string }): string {
  const name = (t.name ?? "").trim();
  const desc = (t.description ?? "").trim();
  const body = name || (desc.length > 28 ? desc.slice(0, 28) + "…" : desc) || "Untitled task";
  return `#${t.id} ${body}`; // id (disambiguation) + name first / truncated description
}

// taskMap builds an id→Task lookup for resolving labels from a task list.
export function taskMap(tasks: Task[]): Map<string, Task> {
  return new Map(tasks.map((t) => [t.id, t]));
}
