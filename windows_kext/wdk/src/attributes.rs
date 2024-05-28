extern crate proc_macro;
use proc_macro::TokenStream;
use quote::quote;

// using proc_macro_attribute to declare an attribute like procedural macro

#[proc_macro_attribute]
// _metadata is argument provided to macro call and _input is code to which attribute like macro attaches
pub fn my_custom_attribute(_metadata: TokenStream, _input: TokenStream) -> TokenStream {
    // returning a simple TokenStream for Struct
    TokenStream::from(quote! {struct H{}})
}
