import { api } from "@/api/wails";

// Short synthesized cues, no audio files: [frequency Hz, start s] per note.
// Rising = something started, falling = it stopped.
const SFX = {
  join: [[523, 0], [784, .1]],
  leave: [[784, 0], [523, .1]],
  peerJoin: [[659, 0], [988, .08]],
  peerLeave: [[988, 0], [659, .08]],
  mute: [[440, 0], [330, .06]],
  unmute: [[330, 0], [440, .06]],
  deaf: [[392, 0], [294, .07], [220, .14]],
  undeaf: [[220, 0], [294, .07], [392, .14]],
  streamOn: [[587, 0], [740, .07], [880, .14]],
  streamOff: [[880, 0], [740, .07], [587, .14]],
  msg: [[880, 0], [1320, .05]],
};

// Cues are synthesized in Go, so they play on the speaker picked in the
// settings; WebKitGTK cannot route WebAudio to a chosen device.
export function sfx(name: keyof typeof SFX) {
  api("Tones", SFX[name]).catch(() => {});
}
