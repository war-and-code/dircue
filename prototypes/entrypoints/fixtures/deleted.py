from fastapi import FastAPI
app = FastAPI()
del app
@app.get("/deleted")
def f(): pass
