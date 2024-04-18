
/**
 * Language Definition:
 *
 * input:
 *
 *    [EXPR] [EXPR]...
 *
 * with:
 *
 *    EXPR = [IDENT][COLON][NOT?][VALUE]
 *    NOT = "!"
 *    VALUE = [STRING][BOOL][NUMBER]
 *    STRING = [a-zA-Z\.0-9]
 *    BOOL = true | false
 *    NUMBER = [0-9]+
 *    COLON = ":"
 *
 */

export enum TokenType {
  WHITESPACE = 'WHITESPACE',
  IDENT = 'IDENT',
  COLON = 'COLON',
  STRING = 'STRING',
  NUMBER = 'NUMBER',
  BOOL = 'BOOL',
  NOT = 'NOT',
  GROUPBY = 'GROUPBY',
  ORDERBY = 'ORDERBY'
}

export type TokenValue<T extends TokenType> =
  T extends TokenType.NUMBER ? number :
  T extends TokenType.STRING ? string :
  T extends TokenType.BOOL ? boolean :
  T extends TokenType.NOT ? '!' :
  T extends TokenType.GROUPBY ? 'string' :
  string;

export interface Token<T extends TokenType> {
  type: T;
  literal: string;
  value: TokenValue<T>;
  start: number;
}
