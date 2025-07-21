from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from youtube_transcript_api import YouTubeTranscriptApi, TranscriptsDisabled, NoTranscriptFound, VideoUnavailable

app = FastAPI()

class VideoRequest(BaseModel):
    video_id: str

@app.post("/get_transcript")
async def get_transcript(request: VideoRequest):
    """
    Accepts a video_id and returns the transcript for that video.
    """
    try:
        transcript_list = YouTubeTranscriptApi.get_transcript(request.video_id, languages=['en', 'en-US', 'en-GB'])
        transcript_text = " ".join([seg['text'] for seg in transcript_list])
        return {"transcript": transcript_text}
    except NoTranscriptFound:
        try:
            transcript_list = YouTubeTranscriptApi.list_transcripts(request.video_id)
            transcript = transcript_list.find_transcript(['en', 'en-US', 'en-GB']).fetch()
            transcript_text = " ".join([seg['text'] for seg in transcript])
            return {"transcript": transcript_text}
        except (TranscriptsDisabled, VideoUnavailable, NoTranscriptFound) as e:
            raise HTTPException(status_code=404, detail=str(e))
    except (TranscriptsDisabled, VideoUnavailable) as e:
        raise HTTPException(status_code=404, detail=str(e))
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"An unexpected error occurred: {str(e)}")

@app.get("/health")
async def health_check():
    return {"status": "ok"}