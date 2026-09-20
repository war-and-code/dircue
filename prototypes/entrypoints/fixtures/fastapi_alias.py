from fastapi import FastAPI as API, APIRouter
app = API()
router = APIRouter(prefix="/api")
@app.get("/items")
def items():
    return []
@router.post(PREFIX + "/dynamic")
def dynamic():
    return None
@router.get("/detail")
def detail():
    return None
app.include_router(router, prefix="/v2")
# @app.get("/comment")
example = '@app.get("/string")'
