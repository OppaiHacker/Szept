// Shapes of what the Go App service (api.go) returns. Field names follow the
// json tags there; keep both sides in step.

export interface BootInfo {
  stage: "setup" | "pass" | "link" | "ready";
  profile: string;
  profiles: string[];
  connector: string;
  accent: string;
  pinned: boolean;
  mobile: boolean;
}

export interface UpdateInfo {
  version: string;
  installed: boolean;
  err: string;
}

export interface Me {
  pub: string;
  name: string;
  code: string;
  status: string;
  bio: string;
  style: string;
  avatar: string;
  banner: string;
  profile: string;
  connector: string;
  connUp: boolean;
  connErr: string;
  connVer: string;
  connRelay: boolean;
  lang: string;
  accent: string;
  dev: boolean;
}

export interface CallInfo {
  joined: boolean;
  mic: boolean;
  deaf: boolean;
  cam: boolean;
  screen: boolean;
  members: string[];
}

export interface Channel {
  id: string;
  name: string;
  kind: "text" | "voice" | "cat";
  cat: string;
  arch: boolean;
}

export interface Chat {
  id: string;
  name: string;
  avatar: string;
  banner: string;
  style: string;
  peer: string;
  direct: boolean;
  members: number;
  online: number;
  inCall: string[];
  call?: CallInfo;
  isOwner: boolean;
  history: boolean;
  callKey: string;
  channels: Channel[];
  voice: Record<string, string[]>;
  vflags: Record<string, string[]>;
  chanUnread: Record<string, number>;
  unread: number;
  preview: string;
  previewTS: number;
}

export interface Friend {
  pub: string;
  name: string;
  alias?: string;
  online: boolean;
  relay: string;
  avatar: string;
  status: string;
  style: string;
  chat: string;
}

export interface Snapshot {
  me: Me;
  chats: Chat[];
  friends: Friend[];
  unread: number;
}

export interface Msg {
  id: string;
  author: string;
  name: string;
  text: string;
  ts: number;
  kind: string; // text | file | info
  info?: string;
  mine: boolean;
  deleted?: boolean;
  pinned?: boolean;
  reply?: string;
  replyName?: string;
  replyText?: string;
  reactions?: Record<string, string>; // pub -> emoji
  ack?: string; // "" | delivered | read
  avatar?: string;
  blob?: string;
  mime?: string;
  file?: string;
  size?: number;
  w?: number;
  h?: number;
  have?: boolean;
  prev?: string;
  havePrev?: boolean;
  getting?: boolean;
  src?: string;
}

export interface Member {
  pub: string;
  name: string;
  avatar: string;
  status: string;
  style: string;
  online: boolean;
  owner: boolean;
  me: boolean;
  inCall: boolean;
  flags: string;
  relay: string;
}

export interface Conversation {
  id: string;
  name: string;
  avatar: string;
  direct: boolean;
  peer: string;
  owner: string;
  isOwner: boolean;
  epoch: number;
  members: Member[];
  msgs: Msg[];
  pinned: Msg[];
  typing: string[];
  hidden: number;
  call: CallInfo;
  online: number;
  callKey: string;
}

export interface InviteCode {
  code: string;
  pin: string;
  qr: string;
}

export interface ProfileCard {
  pub: string;
  name: string;
  status: string;
  bio: string;
  style: string;
  avatar: string;
  banner: string;
  online: boolean;
  isFriend: boolean;
  isMe: boolean;
  chat: string;
  history: { name: string; ts: number }[];
  version: string;
}

export interface Crop {
  X: number;
  Y: number;
  W: number;
  H: number;
}

export interface VoiceOpts {
  no_denoise?: boolean;
  no_agc?: boolean;
  no_comp?: boolean;
  no_rx_protect?: boolean;
  no_gate?: boolean;
  no_monitor?: boolean;
  bitrate?: number;
  comp_preset?: string;
  comp_thresh?: number;
  comp_ratio?: number;
  comp_attack?: number;
  comp_release?: number;
  comp_makeup?: number;
}

export interface DeviceInfo {
  pub: string;
  name: string;
  ts: number;
  online: boolean;
  me: boolean;
}

export interface Settings {
  connector: string;
  connectors: string[];
  connectorLists: string[];
  mediaDir: string;
  accent: string;
  outputVol: number;
  camDevice: string;
  noNotify: boolean;
  noUpdate: boolean;
  relayOnly: boolean;
  quietLookup: boolean;
  lang: string;
  profile: string;
  profiles: string[];
  pinned: boolean;
  webhookOn: boolean;
  webhookPort: number;
  webhookToken: string;
  dataDir: string;
  devMode: boolean;
  micDevice: string;
  speakerDevice: string;
  micGain: number;
  voice: VoiceOpts;
  devices: DeviceInfo[];
  linked: boolean;
}

export interface LinkCode {
  code: string;
  qr: string;
}

export interface AudioDevice {
  id: string;
  name: string;
}

export interface VideoOpts {
  fps?: number;
  height?: number;
  quality?: number;
}
