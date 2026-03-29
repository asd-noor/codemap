// Package scanner provides Tree-sitter query strings used to extract named
// symbol declarations from each supported language.
package scanner

// queries maps each language name to its Tree-sitter S-expression query.
// Each query must produce two captures: @name (the symbol identifier) and
// a @definition.* capture that marks the full declaration node.
var queries = map[string]string{
	"go": `
(function_declaration
    name: (identifier) @name) @definition.function

(method_declaration
    name: (field_identifier) @name) @definition.method

(type_declaration
    (type_spec name: (type_identifier) @name)) @definition.type
`,

	"python": `
(function_definition
    name: (identifier) @name) @definition.function

(class_definition
    name: (identifier) @name) @definition.class
`,

	"javascript": `
(function_declaration
    name: (identifier) @name) @definition.function

(method_definition
    name: (property_identifier) @name) @definition.method

(class_declaration
    name: (identifier) @name) @definition.class

(lexical_declaration
    (variable_declarator
        name: (identifier) @name
        value: [(arrow_function) (function_expression)])) @definition.function

(variable_declaration
    (variable_declarator
        name: (identifier) @name
        value: [(arrow_function) (function_expression)])) @definition.function
`,

	"typescript": `
(function_declaration
    name: (identifier) @name) @definition.function

(method_definition
    name: (property_identifier) @name) @definition.method

(class_declaration
    name: (type_identifier) @name) @definition.class

(interface_declaration
    name: (type_identifier) @name) @definition.interface

(type_alias_declaration
    name: (type_identifier) @name) @definition.type

(lexical_declaration
    (variable_declarator
        name: (identifier) @name
        value: [(arrow_function) (function_expression)])) @definition.function
`,

	"lua": `
(function_declaration
    name: (identifier) @name) @definition.function

(function_declaration
    name: (dot_index_expression) @name) @definition.method

(assignment_statement
    (variable_list (identifier) @name)
    (expression_list (function_definition))) @definition.function
`,

	"zig": `
(function_declaration
    name: (identifier) @name) @definition.function
`,

	"templ": `
(component_declaration
    name: (component_identifier) @name) @definition.function

(css_declaration
    name: (css_identifier) @name) @definition.function

(script_declaration
    name: (script_identifier) @name) @definition.function

(function_declaration
    name: (identifier) @name) @definition.function
`,
}
