import { Token, TokenType } from "./token";

export function isValueToken(tok: Token<any>): tok is Token<TokenType.STRING | TokenType.BOOL | TokenType.NUMBER> {
  return [TokenType.STRING, TokenType.BOOL, TokenType.NUMBER].includes(tok.type)
}

export function isDigit(x: string): boolean {
  return /[0-9]+/.test(x);
}

export function isWhitespace(ch: string): boolean {
  return /\s/.test(ch)
}

export function isLetter(ch: string): boolean {
  return new RegExp('[\/a-zA-Z0-9\._-]').test(ch)
}

export function isIdentChar(ch: string): boolean {
  return /[a-zA-Z_]/.test(ch);
}
