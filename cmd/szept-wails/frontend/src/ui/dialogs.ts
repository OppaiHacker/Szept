// The two dialogs everything else asks through.
import ConfirmDialog from "@/components/dialogs/ConfirmDialog.vue";
import PromptDialog from "@/components/dialogs/PromptDialog.vue";
import { openModal } from "./modal";

export async function confirmBox(title: string, text: string, danger = true, ok?: string): Promise<boolean> {
  return !!(await openModal<boolean>(ConfirmDialog, { title, text, danger, ok }));
}

export function promptBox(o: { title: string; label: string; value?: string; placeholder?: string; hint?: string; multiline?: boolean; ok?: string }) {
  return openModal<string>(PromptDialog, o);
}
