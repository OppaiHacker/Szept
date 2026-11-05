import { reactive, shallowReactive } from "vue";
import type { BootInfo, Conversation, LinkCode, Msg, Settings, Snapshot, UpdateInfo, AudioDevice } from "@/api/types";

// The one app state. Go pushes a "sync" event whenever the DAG changes;
// refresh() pulls a fresh snapshot into here and Vue repaints what moved.
export const S = reactive({
  stage: "loading" as "loading" | "gate" | "ready" | "dead",
  bootErr: "",
  boot: null as BootInfo | null,
  snap: null as Snapshot | null,
  conv: null as Conversation | null,
  version: "dev",
  update: null as UpdateInfo | null,

  // where you are
  space: "home",   // "home" (direct chats + friends), "servers" (phone, none yet) or a group's chat id
  chat: "",        // open chat id
  ch: "",          // open text channel of the group ("" = the default "czat")
  voice: null as string | null, // call key whose voice view is open instead of the chat
  fpage: false,    // the friends page is open (home, no chat)
  ftab: "online" as "online" | "all" | "add",
  filter: "",      // sidebar search
  limit: 60,       // how many messages of the open chat are loaded
  search: null as { q: string; chat: string; hits: Msg[] } | null,
  newMark: null as { chat: string; n: number; id?: string } | null, // where the "nowe" line goes

  // composer
  reply: null as Msg | null,
  files: [] as File[],

  // layout
  showMembers: true,
  collapsed: new Set<string>(),
  hsOpen: false,   // narrow header: the search field is pulled out

  // calls
  focus: null as string | null, // voice view: tile blown up over the others
  full: false,                  // that tile also covers the whole screen
  pip: null as [number, number] | null, // where the floating call window was dragged
  selfMute: false, selfDeaf: false,     // how the next call starts

  // phone
  tab: "" as "" | "you",
  lastGroup: "",
  lastChat: null as [string, string] | null, // closed last, a swipe left reopens it
  mMembers: false,

  // gate
  gateLink: false,

  // settings layer
  settingsOpen: false,
  section: "profile",
  setPaged: false,
  settings: null as Settings | null,
  devices: null as { mics: AudioDevice[]; speakers: AudioDevice[] } | null,
  linkCode: null as LinkCode | null,
  draftStyle: undefined as string | undefined, // profile effects being tried on
  micTest: false,
  camTest: false,
});

// Live call frames: key "<callKey>.<pub>.<c|s>" -> newest frame. Shallow: a
// frame lands up to 24 times a second per stream.
export const frames = shallowReactive(new Map<string, { seq: number; kind: string; name: string; at: number }>());

// Per-person sound in calls, kept for the session: pub -> gain, pan, mute and
// where they sit in the spatial room.
export const peerVol = reactive(new Map<string, { gain: number; pan: number; mute: boolean; pos?: [number, number] }>());

// pubs talking right now (the speaking ring)
export const talking = shallowReactive(new Set<string>());
