// The public key in groups of four hex digits. Two people read it to each
// other over another channel; if it matches, no connector sits in between.
export const keyGroups = (pub: string) => String(pub || "").toLowerCase().match(/[0-9a-f]{4}/g) || [];
export const keyShort = (pub: string) => keyGroups(pub).slice(0, 2).join(" ");
