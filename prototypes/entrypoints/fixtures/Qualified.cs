class Qualified {
 [Microsoft.AspNetCore.Mvc.HttpGet("/qualified")] void F() {}
 [Microsoft.AspNetCore.Mvc.HttpGet($"/{variable}")] void Dynamic() {}
}
