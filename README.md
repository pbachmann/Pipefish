# 🧿 Charm

Welcome to Charm! Charm is a new language. This is version 0.4, a working prototype; you shouldn't use it in production but it's good enough at this point for you to install it and play around with it.

But what *is* Charm? From a theoretical point of view, Charm is a lightweight data-oriented language where the [Functional-Core/Imperative-Shell](https://github.com/tim-hardcastle/Charm/blob/main/docs/functional-core-imperative-shell.md) pattern isn't just a good idea to be maintained by programmer discipline, but rather is a semantic guarantee of the language. From a *practical* point of view, Charm is a delightful little general-purpose language particularly suitable for rapid development of CRUD apps. With the semantics of a functional language, syntax borrowed from other productivity languages (specifically Python and Go), and with [inspiration mainly from SQL and Excel](https://github.com/tim-hardcastle/Charm/blob/main/docs/charm-a-high-level-view.md) — Charm is not *quite* like anything you've seen. But it is also a very practical language that exists to solve some very ordinary and even boring "white-collar" problems.

It is my hope that either Charm itself will one day be used in production, or (given my amateur status and lack of time) that this project will get enough attention that my ideas will be copied by people with more money and personnel and expertise. To this end, please add a star to the repo! Thank you!

Instructions for installing Charm can be found [here](https://github.com/tim-hardcastle/Charm/wiki/Installing-and-using-Charm), as part of [a general manual/tutorial wiki](https://github.com/tim-hardcastle/Charm/wiki) that tells you everything you need to know to code in Charm. There are a number of other documents here, and people who want to just dive in headfirst might want to look at the tutorial document *Writing an adventure game in Charm*.

Here are some of Charm's more distinctive features:

* Charm services have a functional-core/imperative-shell architecture, in which a thin layer of IO sits on top of pure functional business logic.
* All values are immutable; all comparison is by value.
* Functions are pure and referentially transparent.
* Local constants of functions are defined in a block at the end of the function and evaluated only if/when required. (You don't know how nice this is until you've tried it.)
* Free order of intitialization also helps you to write your scripts top-down.
* Abstraction is achieved by overloading and duck-typing. There is multiple dispatch.
* Field names of structs are first-class objects. Indexing structs and maps overloads the same operator.
* Charm is REPL-oriented, with hotcoding to make it easy to code and test incrementally.
* The REPL is also a development environment and framework. It lets you test your code, write permanent tests, ask for help, interact with error messages, configure your services, deploy them to the web and manage access to them.
* It is intended that often a Charm service will act as its own front end (like e.g. a SQL database does) with the end-user talking to it via the Charm REPL. For this reason Charm has an unusually flexible syntax for creating DSLs.
* Charm comes with Go and SQL interop for all your backend needs.
* (Also the system for embedding other languages is extensible if this does not in fact met all your needs.)
* Charm allows and encourages you to write your applications as microservices, giving you a natural way to encapsulate data and manage access to it.
* Charm’s syntax is based (to the extent a functional language can or should be) on imperative productivity languages, principally Go and Python, and will be more familiar to the average programmer than most functional languages.
