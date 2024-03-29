import { isDigit, isIdentChar, isLetter, isWhitespace } from "./helper";
import { InputStream } from "./input";
import { Token, TokenType } from "./token";

export class Lexer {
  private _current: Token<any> | null = null;
  private _input: InputStream;

  constructor(input: string) {
    this._input = new InputStream(input);
  }

  /** peek returns the token at the current position in input. */
  public peek(): Token<any> | null {
    return this._current || (this._current = this.readNextToken());
  }

  /** next returns either the current token in input or reads the next one */
  public next(): Token<any> | null {
    let tok = this._current;
    this._current = null;
    return tok || this.readNextToken();
  }

  /** eof returns true if the lexer reached the end of the input stream */
  public eof(): boolean {
    return this.peek() === null;
  }

  /** croak throws and error message at the current position in the input stream */
  public croak(msg: string): never {
    return this._input.croak(`${msg}. Current token is "${!!this.peek() ? this.peek()!.literal : null}"`);
  }

  /** consumes the input stream as long as predicate returns true */
  private readWhile(predicate: (ch: string) => boolean): string {
    let str = '';
    while (!this._input.eof() && predicate(this._input.peek())) {
      str += this._input.next();
    }

    return str;
  }

  /** reads a number token */
  private readNumber(): Token<TokenType.NUMBER> | null {
    const start = this._input.pos;

    let has_dot = false;
    let number = this.readWhile((ch: string) => {
      if (ch === '.') {
        if (has_dot) {
          return false;
        }

        has_dot = true;
        return true;
      }
      return isDigit(ch);
    });

    if (!this._input.eof() && !isWhitespace(this._input.peek())) {
      this._input.revert(number.length);

      return null;
    }

    return {
      type: TokenType.NUMBER,
      literal: number,
      value: has_dot ? parseFloat(number) : parseInt(number),
      start
    }
  }

  private readIdent(): Token<TokenType.IDENT | TokenType.BOOL | TokenType.GROUPBY | TokenType.ORDERBY> {
    const start = this._input.pos;

    const id = this.readWhile(ch => isIdentChar(ch));
    if (id === 'true' || id === 'yes') {
      return {
        type: TokenType.BOOL,
        literal: id,
        value: true,
        start
      }
    }
    if (id === 'false' || id === 'no') {
      return {
        type: TokenType.BOOL,
        literal: id,
        value: false,
        start
      }
    }
    if (id === 'groupby') {
      return {
        type: TokenType.GROUPBY,
        literal: id,
        value: id,
        start
      }
    }
    if (id === 'orderby') {
      return {
        type: TokenType.ORDERBY,
        literal: id,
        value: id,
        start
      }
    }

    return {
      type: TokenType.IDENT,
      literal: id,
      value: id,
      start
    };
  }

  private readEscaped(end: string | RegExp, skipStart: boolean): string {
    let escaped = false;
    let str = '';

    if (skipStart) {
      this._input.next();
    }

    while (!this._input.eof()) {
      let ch = this._input.next()!;
      if (escaped) {
        str += ch;
        escaped = false;
      } else if (ch === '\\') {
        escaped = true;
      } else if ((typeof end === 'string' && ch === end) || (end instanceof RegExp && end.test(ch))) {
        break;
      } else {
        str += ch;
      }
    }
    return str;
  }

  private readString(quote: string | RegExp, skipStart: boolean): Token<TokenType.STRING> {
    const start = this._input.pos;
    const value = this.readEscaped(quote, skipStart)
    return {
      type: TokenType.STRING,
      literal: value,
      value: value,
      start
    }
  }

  private readWhitespace(): Token<TokenType.WHITESPACE> {
    const start = this._input.pos;
    const value = this.readWhile(ch => isWhitespace(ch));
    return {
      type: TokenType.WHITESPACE,
      literal: value,
      value: value,
      start,
    }
  }

  private readNextToken(): Token<any> | null {
    const start = this._input.pos;
    const ch = this._input.peek();
    if (ch === '') {
      return null;
    }

    if (isWhitespace(ch)) {
      return this.readWhitespace()
    }

    if (ch === '"') {
      return this.readString('"', true);
    }

    if (ch === '\'') {
      return this.readString('\'', true);
    }

    if (isDigit(ch)) {
      const number = this.readNumber();
      if (number !== null) {
        return number;
      }
    }

    if (ch === ':') {
      this._input.next();
      return {
        type: TokenType.COLON,
        value: ':',
        literal: ':',
        start
      }
    }

    if (ch === '!') {
      this._input.next();
      return {
        type: TokenType.NOT,
        value: '!',
        literal: '!',
        start
      }
    }

    if (isIdentChar(ch)) {
      const ident = this.readIdent();

      const next = this._input.peek();
      if (!this._input.eof() && (!isWhitespace(next) && next !== ':')) {

        // identifiers should always end in a colon or with a whitespace.
        // if neither is the case we are in the middle of a token and are
        // likely parsing a string without quotes.
        this._input.revert(ident.literal.length);

        // read the string and revert by one as we terminate the string
        // at the next WHITESPACE token
        const tok = this.readString(new RegExp('\\s'), false)
        this.revertWhitespace();

        return tok;
      }

      return ident;
    }

    if (isLetter(ch)) {
      const tok = this.readString(new RegExp('\\s'), false)
      // read the string and revert by one as we terminate the string
      // at the next WHITESPACE token
      this.revertWhitespace();

      return tok
    }

    // Failed to handle the input character
    return this._input.croak(`Can't handle character: ${ch}`);
  }

  private revertWhitespace() {
    this._input.revert(1)
    if (!isWhitespace(this._input.peek())) {
      this._input.next();
    }
  }
}
