import { isDevMode } from '@angular/core';
import { isValueToken, isWhitespace } from './helper';
import { Lexer } from './lexer';
import { Token, TokenType } from './token';


export interface ParseResult {
  conditions: {
    [key: string]: (any | { $ne: any })[];
  };
  textQuery: string;
  groupBy?: string[];
  orderBy?: string[];
}

export class Parser {
  /** The underlying lexer used to tokenize the input */
  private lexer: Lexer;

  /** Holds the parsed conditions */
  private conditions: {
    [key: string]: any[];
  } = {};

  /** The last condition that has not yet been terminated. Used for scope-based suggestions */
  private _lastUnterminatedCondition: {
    start: number;
    type: string;
    value: any;
  } | null = null;

  /** A list of remaining strings/identifiers that are not part of a condition */
  private remaining: string[] = [];

  /** Returns the last condition that has not yet been terminated. */
  get lastUnterminatedCondition() {
    return this._lastUnterminatedCondition;
  }

  constructor(input: string) {
    this.lexer = new Lexer(input);
  }

  static aliases: { [key: string]: string } = {
    'provider': 'as_owner',
    'app': 'profile',
    'ip': 'remote_ip',
    'port': 'remote_port'
  }

  /** parse is a shortcut for new Parser(input).process() */
  static parse(input: string): ParseResult {
    return new Parser(input).process();
  }

  /** Process the whole input stream and return the parsed result */
  process(): ParseResult {
    let lastIdent: Token<TokenType.IDENT | TokenType.GROUPBY | TokenType.ORDERBY> | null = null;
    let hasColon = false;
    let not = false;
    let groupBy: string[] = [];
    let orderBy: string[] = [];

    while (true) {
      const tok = this.lexer.next()
      if (tok === null) {
        break;
      }

      if (isDevMode()) {
        console.log(tok)
      }

      // if we find a whitespace token we count it as a termination character
      // for the last unterminated condition.
      if (tok.type === TokenType.WHITESPACE) {
        this._lastUnterminatedCondition = null;
      }

      // Since we allow the user to enter values without quotes the
      // lexer might wrongly declare a "string value" as an IDENT.
      // If we have the pattern <IDENT><COLON><IDENT> we re-classify
      // the last IDENT as a STRING value
      if (!!lastIdent && hasColon && tok.type === TokenType.IDENT) {
        tok.type = TokenType.STRING;
      }

      if (tok.type === TokenType.IDENT || tok.type === TokenType.GROUPBY || tok.type === TokenType.ORDERBY) {
        // if we had an IDENT token before and got a new one now the
        // previous one is pushed to the remaining list
        if (!!lastIdent) {
          this._lastUnterminatedCondition = null;
          this.remaining.push(lastIdent.value)
        }
        lastIdent = tok;
        this._lastUnterminatedCondition = {
          start: tok.start,
          type: Parser.aliases[lastIdent.value] || lastIdent.value,
          value: '',
        }

        continue
      }

      // if we don't have an preceding IDENT token
      // this must be part of remaingin
      if (!lastIdent) {
        this.remaining.push(tok.literal);
        this._lastUnterminatedCondition = null;

        continue
      }

      // we would expect a colon now
      if (!hasColon) {
        if (tok.type !== TokenType.COLON) {
          // we expected a colon but got something else.
          // this means the last IDENT is part of remaining
          this.remaining.push(lastIdent.value);
          lastIdent = null;
          this._lastUnterminatedCondition = null;

          continue
        }

        // we have a colon now so proceed to the next token
        hasColon = true;
        not = false;

        continue
      }

      if (lastIdent.type === TokenType.GROUPBY) {
        groupBy.push(Parser.aliases[tok.literal] || tok.literal)
        lastIdent = null
        hasColon = false

        continue
      }

      if (lastIdent.type == TokenType.ORDERBY) {
        orderBy.push(Parser.aliases[tok.literal] || tok.literal)
        lastIdent = null
        hasColon = false

        continue
      }

      if (tok.type === TokenType.NOT && not === false) {
        not = true

        continue
      }

      if (isValueToken(tok)) {
        let identValue = Parser.aliases[lastIdent.value] || lastIdent.value;

        if (!this.conditions[identValue]) {
          this.conditions[identValue] = [];
        }

        if (!not) {
          this.conditions[identValue].push(tok.value)
        } else {
          this.conditions[identValue].push({ $ne: tok.value })
        }
        this._lastUnterminatedCondition!.value = tok.value;

        lastIdent = null
        hasColon = false
        not = false

        continue
      }

      this.remaining.push(lastIdent.value);
      lastIdent = null;
      hasColon = false;
      not = false;
      this._lastUnterminatedCondition = null;
    }

    if (!!lastIdent) {
      this.remaining.push(lastIdent.value);

      if (hasColon) {
        this._lastUnterminatedCondition = {
          start: lastIdent.start,
          type: Parser.aliases[lastIdent.value] || lastIdent.value,
          value: ''
        };
      } else {
        this._lastUnterminatedCondition = null;
      }
    }

    return {
      groupBy: groupBy.length > 0 ? groupBy : undefined,
      orderBy: orderBy.length > 0 ? orderBy : undefined,
      conditions: this.conditions,
      textQuery: this.remaining.filter(tok => !isWhitespace(tok)).join(" "),
    }
  }
}
