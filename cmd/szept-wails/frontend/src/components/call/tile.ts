// A tile of the voice view: a camera or shared screen (src), or a face.
export interface Tile {
  key: string;
  pub: string;
  name: string;
  avatar?: string;
  flags?: string;
  src?: string;
  screen?: boolean;
  muted?: boolean;
}
