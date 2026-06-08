curl -s --user 'api:any-key' http://localhost:18025/v3/example.com/messages \
    --form-string 'from=Sender <sender@example.com>' \
    --form-string 'to=User <user@example.com>' \
    --form-string 'subject=Async unsubscribe check' \
    --form-string 'text=Plain body' \
    --form-string 'html=<p>Newsletter</p>' \
    --form-string 'h:List-Unsubscribe=<http://localhost:18025/api/messages>' \
    --form-string 'h:List-Unsubscribe-Post=List-Unsubscribe=One-Click'
