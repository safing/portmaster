/** Input stream returns one character at a time */
export class InputStream {
  private _pos: number = 0;
  private _line: number = 0;

  constructor(private _input: string) { }

  /** Returns the next character and removes it from the stream */
  next(): string | null {
    const ch = this._input.charAt(this._pos++);
    return ch;
  }

  get pos() {
    return this._pos;
  }

  /** Revert moves the current stream position back by `num` characters */
  revert(num: number) {
    this._pos -= num;
  }

  /** Returns the next character in the stream but does not remove it */
  peek(): string {
    return this._input.charAt(this._pos);
  }

  /** Returns true if we reached the end of the stream */
  eof(): boolean {
    return this.peek() == '';
  }

  get left(): string {
    return this._input.slice(this._pos)
  }

  /** Throws an error with the current line and column */
  croak(msg: string): never {
    throw new Error(`${msg} at ${this._line}:${this.pos}`);
  }
}
