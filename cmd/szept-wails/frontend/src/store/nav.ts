import { watch } from "vue";
import { S } from "./state";
import { leaveChat, openChat, openVoice } from "./actions";

// Back and forward, like a browser: the mouse side buttons (and Alt+arrows)
// walk through the places you opened.
const nav = { list: [] as string[], at: -1, busy: false };
const navKey = () => JSON.stringify([S.space, S.chat, S.ch, S.voice, S.fpage]);

watch(navKey, (k) => {
  if (nav.busy || S.stage !== "ready" || nav.list[nav.at] === k) return;
  nav.list = nav.list.slice(0, nav.at + 1).concat(k).slice(-50);
  nav.at = nav.list.length - 1;
});

async function goNav(step: number) {
  const k = nav.list[nav.at + step];
  if (!k) return;
  nav.at += step;
  nav.busy = true;
  try {
    const [space, chat, ch, voice, fpage] = JSON.parse(k);
    if (!chat || !S.snap?.chats.some((c) => c.id === chat)) leaveChat({ space, fpage });
    else { await openChat(chat, ch); if (voice) await openVoice(voice); }
  } finally { nav.busy = false; }
}

document.addEventListener("mouseup", (e) => {
  if (e.button === 3 || e.button === 4) { e.preventDefault(); goNav(e.button === 3 ? -1 : 1); }
});
document.addEventListener("keydown", (e) => {
  if (e.altKey && (e.key === "ArrowLeft" || e.key === "ArrowRight")) { e.preventDefault(); goNav(e.key === "ArrowLeft" ? -1 : 1); }
});
