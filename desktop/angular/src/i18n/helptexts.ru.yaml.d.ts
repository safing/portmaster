declare const helptexts: {
  [key: string]: {
    title: string;
    content: string;
    url?: string;
    urlText?: string;
    nextKey?: string;
    buttons?: Array<{
      name: string;
      action: any;
      nextKey?: string;
    }>;
  };
};
export default helptexts;
