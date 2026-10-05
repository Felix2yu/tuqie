export type Axis = 'y' | 'x';

export type Candidate = {
  pos: number;
  score: number;
  kind: 'seam' | 'gap' | string;
  blank: boolean;
};

export type Signal = {
  step: number;
  lines: number;
  diff: number[];
  flat: number[];
};

export type Analysis = {
  id: string;
  filename: string;
  mime: string;
  width: number;
  height: number;
  /** Which way the screenshot was stitched, and therefore which way the cuts run. */
  axis: Axis;
  url: string;
  candidates: Candidate[];
  signal: Signal;
};

export type Format = 'jpeg' | 'png';

export type Settings = {
  format: Format;
  quality: number;
};
