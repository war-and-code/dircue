class Qualified {
 @org.springframework.web.bind.annotation.GetMapping(path="/qualified") void f() {}
 @org.springframework.web.bind.annotation.GetMapping({"/one", "/two"}) void array() {}
}
