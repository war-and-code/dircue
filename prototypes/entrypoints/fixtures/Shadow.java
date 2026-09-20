import org.springframework.web.bind.annotation.GetMapping;
@interface GetMapping { String value(); }
class Shadow { @GetMapping("/shadow") void f() {} }
