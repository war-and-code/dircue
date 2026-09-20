using Microsoft.AspNetCore.Mvc;
using Read = Microsoft.AspNetCore.Mvc.HttpGetAttribute;
[Route("api/[controller]")]
class AliasController : UnknownBase {
    [Read("items")] public string Items() => "ok";
    [HttpPost(Prefix + "/dynamic")] public string Add() => "ok";
    public void Configure() { arbitrary.MapGet("/candidate", () => "ok"); }
    // [HttpGet("comment")]
    string fake = "[HttpGet(\"string\")]";
}
