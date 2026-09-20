import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
@RequestMapping("/api")
class SpringExample extends UnknownBase {
    @GetMapping("/items") String items() { return "ok"; }
    @GetMapping(PREFIX + "/dynamic") String dynamic() { return "ok"; }
    @GetMapping("${configured}/items") String configured() { return "ok"; }
    // @GetMapping("/comment")
    String fake = "@GetMapping(\"/string\")";
}
